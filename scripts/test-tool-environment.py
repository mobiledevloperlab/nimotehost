#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Exercise tool discovery in an isolated native Host; no service installation."""
import argparse
import base64
import json
import os
import pathlib
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--binary', required=True, help='Local Host executable to verify')
parser.add_argument('--check-homebrew-codex', action='store_true', help='Also require the installed /opt/homebrew/bin/codex fallback')
args = parser.parse_args()
if sys.platform not in ('darwin', 'linux'):
    parser.error('This process/session fixture requires macOS or Linux; Windows has native Go tests.')
binary = str(pathlib.Path(args.binary).resolve(strict=True))
root = pathlib.Path(tempfile.mkdtemp(prefix='nimotehost-tool-env-integration-'))
home = root / 'home'
state = (home / 'Library' / 'Application Support' / 'nimotehost' if sys.platform == 'darwin'
         else home / '.config' / 'nimotehost')
state.mkdir(parents=True, mode=0o700)
os.chmod(state, 0o700)
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]

def write_json(path, data):
    path.write_text(json.dumps(data))
    os.chmod(path, 0o600)

write_json(state / 'config.json', {'port': port, 'log_level': 'error'})
# Disable shell discovery deliberately: this proves Homebrew fallback works even
# if shell startup scripts cannot be loaded, as well as with a system-only PATH.
initial_config = {'disable_shell': True}
if args.check_homebrew_codex:
    if sys.platform != 'darwin' or not pathlib.Path('/opt/homebrew/bin/codex').is_file():
        parser.error('--check-homebrew-codex requires an installed macOS Homebrew Codex CLI')
    expected_codex, expected_source = '/opt/homebrew/bin/codex', 'platform'
else:
    initial_bin = root / 'initial tools'
    initial_bin.mkdir()
    initial_cli = initial_bin / 'codex'
    initial_cli.write_text('#!/bin/sh\nprintf initial-cli-version\n')
    os.chmod(initial_cli, 0o700)
    initial_config['search_paths'] = [str(initial_bin)]
    expected_codex, expected_source = str(initial_cli), 'configured'
write_json(state / 'tools.json', initial_config)
env = dict(os.environ, HOME=str(home), XDG_CONFIG_HOME=str(home / '.config'),
           CODEX_HOME=str(home / '.codex'), PATH='/usr/bin:/bin:/usr/sbin:/sbin')
log = open(root / 'host.log', 'wb')
host = subprocess.Popen([binary, 'start'], env=env, stdout=log, stderr=log)

def cli(*args):
    result = subprocess.run([binary, *args], env=env, capture_output=True, timeout=15)
    if result.returncode:
        raise AssertionError((args, result.stderr.decode()))
    return json.loads(result.stdout)

def rpc(method, path, body=None):
    token = (state / 'token').read_text()
    req = urllib.request.Request('http://127.0.0.1:%d%s' % (port, path),
        data=None if body is None else json.dumps(body).encode(), method=method,
        headers={'Authorization': 'Bearer ' + token, 'Nimote-Host-Protocol': '1', 'Content-Type': 'application/json'})
    with urllib.request.urlopen(req, timeout=15) as response:
        return json.load(response)

try:
    deadline = time.monotonic() + 12
    while True:
        if host.poll() is not None:
            raise AssertionError((root / 'host.log').read_text())
        try:
            caps = cli('capabilities')
            break
        except AssertionError:
            if time.monotonic() >= deadline:
                raise
            time.sleep(0.05)
    codex = caps['agents']['codex']
    assert codex['installed'] and codex['path'] == expected_codex, codex
    assert codex['path_source'] == expected_source, codex
    assert caps['features']['codex_history_v1'] and caps['features']['tool_environment_v1']
    print('PASS: system-only PATH discovers Codex in the daemon (source: %s)' % expected_source, flush=True)

    # Make the diagnostic caller see a different CLI than the daemon.
    caller_bin = root / 'caller-bin'
    caller_bin.mkdir()
    (caller_bin / 'codex').write_text('#!/bin/sh\nprintf caller-only-version\n')
    os.chmod(caller_bin / 'codex', 0o700)
    caller_env = dict(env, PATH=str(caller_bin) + ':' + env['PATH'])
    result = subprocess.run([binary, 'agents'], env=caller_env, capture_output=True, timeout=15, check=True)
    assert json.loads(result.stdout)['codex']['path'] == expected_codex
    print('PASS: agents probes the daemon rather than the caller PATH')

    tools = root / 'node installation with spaces'
    tools.mkdir()
    (tools / 'node').write_text('#!/bin/sh\nprintf fixture-node-runtime\n')
    (tools / 'codex').write_text('#!/usr/bin/env node\n')
    for path in tools.iterdir():
        os.chmod(path, 0o700)
    sleeper = rpc('POST', '/v1/sessions', {'kind': 'task', 'command': '/bin/sleep', 'args': ['20'], 'directory': str(home)})
    write_json(state / 'tools.json', {'disable_shell': True, 'executables': {'codex': str(tools / 'codex')}})
    status = cli('tools', 'refresh')
    assert status['executables']['codex'] == str(tools / 'codex')
    assert rpc('GET', '/v1/sessions/' + sleeper['id'])['state'] == 'running'
    agents = cli('agents')
    assert agents['codex']['probe_status'] == 'ok' and agents['codex']['version'] == 'fixture-node-runtime', agents['codex']
    session = rpc('POST', '/v1/sessions', {'kind': 'task', 'command': 'codex', 'directory': str(home)})
    deadline = time.monotonic() + 5
    while rpc('GET', '/v1/sessions/' + session['id'])['state'] == 'running':
        assert time.monotonic() < deadline
        time.sleep(0.05)
    output = rpc('GET', '/v1/sessions/' + session['id'] + '/output?offset=0')
    assert base64.b64decode(output['data']) == b'fixture-node-runtime', output
    print('PASS: hot refresh preserves running sessions and launches CLI with sibling Node')

    # A failed refresh must not poison future task launches.
    write_json(state / 'tools.json', {'search_paths': ['relative']})
    failed = subprocess.run([binary, 'tools', 'refresh'], env=env, capture_output=True, timeout=15)
    assert failed.returncode != 0
    assert cli('capabilities')['agents']['codex']['path'] == str(tools / 'codex')
    print('PASS: invalid refresh preserves the previous working environment')

    rpc('POST', '/v1/sessions/' + sleeper['id'] + '/cancel', {})
    rpc('POST', '/v1/shutdown', {})
    host.wait(timeout=10)
    assert host.returncode == 0
    print('PASS: isolated Host shut down cleanly; existing installation untouched')
finally:
    if host.poll() is None:
        host.terminate()
        try:
            host.wait(timeout=10)
        except subprocess.TimeoutExpired:
            host.kill()
            host.wait()
    log.close()
    print('Integration artifacts:', root)

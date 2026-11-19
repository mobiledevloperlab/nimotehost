#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Run on a disposable Linux CI runner with systemd and local OpenSSH.
set -euo pipefail

scratch="$(mktemp -d)"
arch="$(go env GOARCH)"
state="${XDG_CONFIG_HOME:-$HOME/.config}/nimotehost"
[[ ! -e "$state" && ! -e "$HOME/.local/share/nimotehost" ]]
export XDG_RUNTIME_DIR="/run/user/$(id -u)"
export DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/bus"

sudo loginctl enable-linger "$USER"
for attempt in {1..20}; do
    if systemctl --user show-environment >/dev/null 2>&1; then break; fi
    sleep 1
done
systemctl --user show-environment >/dev/null

go run ./tools/build -version 2.4.0-ci.1
cp -R "dist/linux-$arch" "$scratch/first"
bootstrap="$scratch/first/nimotehost"
sudo systemctl start ssh
ssh-keygen -q -t ed25519 -N '' -f "$scratch/key"
mkdir -p -m 700 "$HOME/.ssh"
cat "$scratch/key.pub" >> "$HOME/.ssh/authorized_keys"
chmod 600 "$HOME/.ssh/authorized_keys"
printf '127.0.0.1 %s\n' "$(cat /etc/ssh/ssh_host_ed25519_key.pub)" > "$scratch/known_hosts"
ssh_options=(-T -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes
    -o "UserKnownHostsFile=$scratch/known_hosts" -i "$scratch/key")
printf -v setup_command 'XDG_RUNTIME_DIR=%q DBUS_SESSION_BUS_ADDRESS=%q %q setup' \
    "$XDG_RUNTIME_DIR" "$DBUS_SESSION_BUS_ADDRESS" "$bootstrap"
ssh "${ssh_options[@]}" "$USER@127.0.0.1" "$setup_command"
ssh "${ssh_options[@]}" "$USER@127.0.0.1" "$setup_command"
ssh "${ssh_options[@]}" "$USER@127.0.0.1" \
    '"$HOME/.local/share/nimotehost/nimotehost" status' | jq -e '.running'
"$bootstrap" deploy health | jq -e '.ok and .running and .version == "2.4.0-ci.1"'

set +e
sudo "$bootstrap" deploy install > "$scratch/root.json"
root_exit=$?
set -e
[[ "$root_exit" == 4 ]]
jq -e '.error.code == "permission_denied"' < "$scratch/root.json"

go run ./tools/build -version 2.4.0-ci.2
new_bundle="dist/linux-$arch"
installed="$HOME/.local/share/nimotehost/nimotehost"
session_id="$(jq -nc --arg dir "$scratch" \
    '{method:"POST",path:"/v1/sessions",body:{kind:"task",command:"/bin/sleep",args:["60"],directory:$dir}}' |
    "$installed" rpc | jq -er .id)"
if "$new_bundle/nimotehost" setup; then
    echo 'Setup should refuse to stop a Host with active tasks' >&2
    exit 1
fi
jq -nc --arg id "$session_id" '{method:"POST",path:("/v1/sessions/"+$id+"/cancel")}' | "$installed" rpc
for attempt in {1..10}; do
    session="$(jq -nc --arg id "$session_id" '{method:"GET",path:("/v1/sessions/"+$id)}' | "$installed" rpc)"
    if jq -e '.state == "canceled"' <<< "$session"; then break; fi
    sleep 1
done
jq -e '.state == "canceled"' <<< "$session"
"$new_bundle/nimotehost" setup
"$bootstrap" deploy health | jq -e '.ok and .running and .version == "2.4.0-ci.2"'

# Carry out setup from the genuine executable while staging a verified fixture
# whose install entrypoint fails immediately after the directory switch.
cp -R "$new_bundle" "$scratch/broken"
cp "$new_bundle/nimotehost" "$scratch/broken/setup-driver"
printf '#!/bin/sh\nexit 23\n' > "$scratch/broken/nimotehost"
(
    cd "$scratch/broken"
    sha256sum nimotehost > binary.sha256
    sed -i '/  nimotehost$/d' SHA256SUMS
    cat binary.sha256 >> SHA256SUMS
    rm binary.sha256
    sha256sum setup-driver >> SHA256SUMS
)
if "$scratch/broken/setup-driver" setup; then
    echo 'Expected the new service registration to fail' >&2
    exit 1
fi
"$bootstrap" deploy health | jq -e '.ok and .running and .version == "2.4.0-ci.2"'

# Trigger an abnormal exit from the native manager and observe a new instance.
old_instance="$("$installed" status | jq -er .instance_id)"
systemctl --user kill --signal=SIGKILL nimotehost.service
for attempt in {1..20}; do
    new_instance="$("$installed" status 2>/dev/null | jq -r .instance_id || true)"
    if [[ -n "$new_instance" && "$new_instance" != "$old_instance" ]]; then break; fi
    sleep 1
done
[[ -n "$new_instance" && "$new_instance" != "$old_instance" ]]
"$bootstrap" deploy stop | jq -e '.ok and (.running | not)'
"$bootstrap" deploy start | jq -e '.ok and .running'
"$bootstrap" deploy uninstall | jq -e '.ok and (.installed | not) and (.service_registered | not)'
test -f "$state/token"
"$bootstrap" deploy uninstall --purge-data | jq -e '.ok'
test ! -e "$state"

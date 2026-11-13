"""Check local Markdown links in the maintained documentation trees."""
import pathlib
import re
import unittest
import urllib.parse

ROOT = pathlib.Path(__file__).resolve().parents[1]

class DocumentationTests(unittest.TestCase):
    def test_local_links_exist(self):
        pages = list(ROOT.glob('*.md'))
        pages += list((ROOT / 'docs').rglob('*.md'))
        for page in pages:
            for target in re.findall(r'\]\(([^)]+)\)', page.read_text()):
                target = target.strip('<>')
                if urllib.parse.urlsplit(target).scheme:
                    continue
                path = urllib.parse.unquote(target.split('#', 1)[0])
                if not path:
                    continue
                with self.subTest(page=str(page.relative_to(ROOT)), target=target):
                    self.assertTrue((page.parent / path).exists(), 'Broken local documentation link')

if __name__ == '__main__':
    unittest.main()

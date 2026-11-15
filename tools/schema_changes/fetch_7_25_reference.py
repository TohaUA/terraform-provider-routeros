import concurrent.futures
import hashlib
import json
from html.parser import HTMLParser
from pathlib import Path
from urllib.request import urlopen

import argparse

parser = argparse.ArgumentParser(description="Fetch raw official 7.25 reference tables for manual snapshot review")
parser.add_argument("--output", type=Path, required=True, help="New or existing audit directory; never the repository")
args = parser.parse_args()
ROOT = args.output
ROOT.mkdir(parents=True, exist_ok=True)
manifest = json.loads((Path(__file__).parent / "routeros-7.25-api.json").read_text())
paths = {p for p, menu in manifest["menus"].items() if "sha256" in menu}
with urlopen(manifest["source"], timeout=45) as response:
    comparison = response.read()
(ROOT / "cli-compare.json").write_bytes(comparison)
(ROOT / "delta.json").write_text(json.dumps(json.loads(comparison)["pairs"]["7.24->7.25"], indent=2) + "\n")

class Tables(HTMLParser):
    def __init__(self):
        super().__init__()
        self.tables = []
        self.table = None
        self.row = None
        self.cell = None
    def finish_cell(self):
        if self.cell is not None:
            self.row.append(' '.join(''.join(self.cell).split()))
            self.cell = None
    def finish_row(self):
        self.finish_cell()
        if self.row is not None:
            self.table.append(self.row)
            self.row = None
    def handle_starttag(self, tag, attrs):
        if tag == 'table': self.table = []
        if tag == 'tr' and self.table is not None:
            self.finish_row()
            self.row = []
        if tag in ('th', 'td') and self.row is not None:
            self.finish_cell()
            self.cell = []
        if tag in ('br', 'li') and self.cell is not None: self.cell.append(' ')
    def handle_data(self, data):
        if self.cell is not None: self.cell.append(data)
    def handle_endtag(self, tag):
        if tag in ('th', 'td') and self.cell is not None:
            self.finish_cell()
        if tag == 'tr' and self.row is not None:
            self.finish_row()
        if tag == 'table' and self.table is not None:
            self.finish_row()
            self.tables.append(self.table)
            self.table = None

def fetch(path):
    url = 'https://manual.mikrotik.com/docs/7.25/cli-reference/' + path + '/'
    dest = ROOT / 'pages' / (path.replace('/', '__') + '.html')
    dest.parent.mkdir(exist_ok=True)
    try:
        with urlopen(url, timeout=45) as response: dest.write_bytes(response.read())
        raw = dest.read_bytes()
        parser = Tables()
        parser.feed(raw.decode())
        return path, {'url': url, 'sha256': hashlib.sha256(raw).hexdigest(), 'tables': parser.tables}
    except Exception as exc:
        return path, {'url': url, 'error': str(exc)}

with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
    results = dict(pool.map(fetch, sorted(paths)))
(ROOT / 'reference.json').write_text(json.dumps(results, indent=2) + '\n')
for path, page in results.items():
    print(path, page.get('error', page.get('sha256', '')))
if any('error' in page for page in results.values()):
    raise SystemExit('Some reference pages could not be fetched; do not replace the pinned snapshot')

import hashlib
import json
from pathlib import Path
import sys
from zipfile import ZipFile
from fontTools.ttLib import TTFont

root = Path(__file__).resolve().parent.parent
work = Path(sys.argv[1]) if len(sys.argv) > 1 else root / 'build/font-work'
output = root / 'frontend/src/shared/fonts/assets'
output.mkdir(parents=True, exist_ok=True)
manifest = root / 'scripts/fonts-manifest.json'
if manifest.exists():
    for source in json.loads(manifest.read_text(encoding='utf-8'))['inputs']:
        assert hashlib.sha256((work / source['file']).read_bytes()).hexdigest() == source['sha256'], 'Font source checksum mismatch: ' + source['file']
report = {}
with ZipFile(work / 'google-sans-code.zip') as archive:
    name = next(n for n in archive.namelist() if n.startswith('GoogleSansCode[') and n.endswith('.ttf'))
    (work / 'google-sans-code.ttf').write_bytes(archive.read(name))
with ZipFile(work / 'maple-cn.zip') as archive:
    name = next(n for n in archive.namelist() if n.endswith('MapleMono-CN-Regular.ttf'))
    (work / 'maple-mono-cn.ttf').write_bytes(archive.read(name))
    print('Maple source:', name)
    print('Archive notices:', [n for n in archive.namelist() if 'license' in n.lower() or 'ofl' in n.lower()])
for name, source in [('inter', 'InterVariable.woff2'), ('google-sans-code', 'google-sans-code.ttf'), ('maple-mono-cn', 'maple-mono-cn.ttf')]:
    font = TTFont(work / source, recalcTimestamp=False)
    before = font.getBestCmap()
    target = output / (name + '.woff2')
    if name == 'inter':
        target.write_bytes((work / source).read_bytes())
    else:
        font.flavor = 'woff2'
        font.save(target)
    packed = TTFont(target)
    assert packed.getBestCmap() == before, 'Compression changed character coverage'
    assert packed['hmtx'].metrics == font['hmtx'].metrics, 'Compression changed glyph widths'
    cmap = packed.getBestCmap()
    widths = {c: packed['hmtx'].metrics[cmap[ord(c)]][0] / packed['head'].unitsPerEm for c in '0AW中文繁體日本語あいう' if ord(c) in cmap}
    report[name] = {'bytes': target.stat().st_size, 'sha256': hashlib.sha256(target.read_bytes()).hexdigest(), 'characters': len(cmap), 'widthsEm': widths,
                    'copyright': [str(n.toUnicode()) for n in packed['name'].names if n.nameID == 0][:2]}
(work / 'font-report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
print(json.dumps(report, ensure_ascii=False, indent=2))

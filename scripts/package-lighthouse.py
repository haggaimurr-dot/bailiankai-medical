"""Package built application artifacts without local secrets or patient data."""
from pathlib import Path
import hashlib
import shutil
import tarfile

root = Path(__file__).resolve().parent.parent
output = root / '.local' / 'lighthouse-20261023'
release = output / 'release'
if not (release / 'bin' / 'api').exists():
    raise SystemExit('Build Linux binaries before packaging')
if not (root / 'apps/admin/dist/index.html').exists():
    raise SystemExit('Build the admin site before packaging')
shutil.copytree(root / 'apps/admin/dist', release / 'admin', dirs_exist_ok=True)
shutil.copytree(root / 'deploy/lighthouse', release / 'deploy', dirs_exist_ok=True)
archive_path = output / 'bailiankai-release.tar.gz'
with tarfile.open(archive_path, 'w:gz') as archive:
    for path in sorted(release.rglob('*')):
        if not path.is_file():
            continue
        relative = path.relative_to(release)
        info = archive.gettarinfo(str(path), arcname=relative.as_posix())
        info.uid = info.gid = 0
        info.uname = info.gname = 'root'
        info.mode = 0o755 if relative.parts[0] == 'bin' or path.suffix == '.sh' else 0o644
        with path.open('rb') as contents:
            archive.addfile(info, contents)
digest = hashlib.sha256(archive_path.read_bytes()).hexdigest()
(output / 'bailiankai-release.sha256').write_text(f'{digest}  {archive_path.name}\n')
print(f'Package: {archive_path}\nSHA256: {digest}\nBytes: {archive_path.stat().st_size}')

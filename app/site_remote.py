"""Edit a Krot-managed nginx document root over SSH."""
import ctypes
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

WEB = Path("/var/www")
ENABLED = Path("/etc/nginx/sites-enabled")
PREFIX = "vpnkit-"
MAX_HTML = 1024 * 1024
DOMAIN = re.compile(r"[a-z0-9-]+(?:\.[a-z0-9-]+)+")


def managed(domain):
    if len(domain) > 253 or not DOMAIN.fullmatch(domain):
        return False
    root = WEB / (PREFIX + domain)
    config = ENABLED / (PREFIX + domain)
    return (root.is_dir() and not root.is_symlink() and config.is_file()
            and f"root {root};" in config.read_text(encoding="utf-8"))


def index_bytes(root):
    index = root / "index.html"
    if not index.is_file() or index.is_symlink():
        raise ValueError("в корне сайта нет обычного файла index.html")
    with index.open("rb") as file:
        data = file.read(MAX_HTML + 1)
    if len(data) > MAX_HTML:
        raise ValueError("index.html больше 1 МБ. Используйте публикацию папки.")
    return data


def extract_site(stream, stage):
    with tarfile.open(fileobj=stream, mode="r|gz") as archive:
        for member in archive:
            rel = PurePosixPath(member.name)
            if not rel.parts or rel.is_absolute() or ".." in rel.parts or "\\" in member.name:
                raise ValueError("архив содержит недопустимый путь")
            dst = stage.joinpath(*rel.parts)
            if member.isdir():
                dst.mkdir(parents=True, exist_ok=True)
            elif member.isreg():
                dst.parent.mkdir(parents=True, exist_ok=True)
                with archive.extractfile(member) as source, dst.open("xb") as target:
                    shutil.copyfileobj(source, target)
            else:
                raise ValueError("архив содержит ссылку или специальный файл")
    index = stage / "index.html"
    if not index.is_file() or index.stat().st_size == 0:
        raise ValueError("в корне сайта нужен непустой index.html")


def exchange(left, right):
    rename = ctypes.CDLL(None, use_errno=True).renameat2
    rename.argtypes = (ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint)
    rename.restype = ctypes.c_int
    # AT_FDCWD and RENAME_EXCHANGE (Linux). Both directories stay on the same filesystem.
    if rename(-100, os.fsencode(left), -100, os.fsencode(right), 2) != 0:
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error))


def digest(path):
    with path.open("rb") as file:
        return hashlib.file_digest(file, "sha256").hexdigest()


def check_site(domain, root, scratch):
    response = scratch / "response"
    code = subprocess.check_output(["curl", "--silent", "--show-error", "--noproxy", "*",
                                    "--resolve", f"{domain}:443:127.0.0.1", "--max-time", "12",
                                    "--output", str(response), "--write-out", "%{http_code}",
                                    f"https://{domain}/index.html"], text=True, timeout=15)
    if code != "200" or digest(response) != digest(root / "index.html"):
        raise ValueError("nginx не отдал опубликованный index.html через HTTPS")


def publish(action, domain, root, version, stream):
    state = WEB / ".krot-site-editor" / domain
    state.mkdir(parents=True, exist_ok=True)
    # The lock and staging directories sit outside the configured nginx roots.
    state.parent.chmod(0o700)
    with (state / "lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        scratch = Path(tempfile.mkdtemp(dir=state))
        keep = False
        try:
            stage = scratch / "site"
            if action == "html":
                old = index_bytes(root)
                if hashlib.sha256(old).hexdigest() != version:
                    raise ValueError("index.html уже изменён. Откройте редактор снова.")
                data = stream.read(MAX_HTML + 1)
                if not data or len(data) > MAX_HTML:
                    raise ValueError("для редактора нужен непустой index.html до 1 МБ")
                data.decode("utf-8")
                shutil.copytree(root, stage, symlinks=True, copy_function=os.link)
                (stage / "index.html").unlink()
                (stage / "index.html").write_bytes(data)
            else:
                stage.mkdir()
                extract_site(stream, stage)
            exchange(root, stage)
            try:
                check_site(domain, root, scratch)
            except (OSError, ValueError, subprocess.SubprocessError):
                try:
                    exchange(root, stage)
                except OSError as error:
                    keep = True
                    raise ValueError(f"автооткат не удался. Предыдущие файлы сохранены в {stage}.") from error
                raise ValueError("проверка через HTTPS не прошла. Предыдущая версия восстановлена.")
        finally:
            if not keep:
                shutil.rmtree(scratch)


def main(args):
    action = args[0]
    if action == "list":
        names = (path.name.removeprefix(PREFIX) for path in ENABLED.glob(PREFIX + "*"))
        print(json.dumps(sorted(domain for domain in names if managed(domain))))
        return
    domain = args[1]
    if not managed(domain):
        raise ValueError("на узле нет включённого сайта Крота с таким доменом")
    root = WEB / (PREFIX + domain)
    if action == "read":
        data = index_bytes(root)
        print(json.dumps({"html": data.decode("utf-8"), "version": hashlib.sha256(data).hexdigest()}))
    elif action in {"html", "directory"}:
        os.umask(0o022)
        publish(action, domain, root, args[2] if action == "html" else None, sys.stdin.buffer)


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except (ValueError, OSError, tarfile.TarError, AttributeError) as error:
        sys.exit(str(error))

#!/usr/bin/env python3
"""Update an existing PostgreSQL Compose deployment; Python standard library only."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import uuid
from datetime import datetime, timezone
from urllib.parse import urlsplit, unquote
from urllib.request import urlopen

RUNTIME = ("postgres", "redis", "yingce-agent", "backend", "web")
APP = ("web", "backend", "yingce-agent")
UPDATER = "open-ai-canvas-updater.service"


class DeployError(RuntimeError):
    pass


def run(args, *, capture=False, stdout=None):
    result = subprocess.run(args, text=stdout is None, stdout=subprocess.PIPE if capture else stdout,
                            stderr=subprocess.PIPE if capture else None)
    if result.returncode:
        # Compose's resolved configuration and environment must never reach the console.
        raise DeployError(f"Command failed ({result.returncode}): {args[0]} {args[1] if len(args) > 1 else ''}")
    return result.stdout if capture else None


def json_run(args):
    return json.loads(run(args, capture=True))


def dump(path, value):
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    path.chmod(0o600)


def compose_literal(value):
    """Resolved configs need dollars escaped before Compose reads them again."""
    if isinstance(value, str):
        return value.replace("$", "$$")
    if isinstance(value, dict):
        return {k: compose_literal(v) for k, v in value.items()}
    if isinstance(value, list):
        return [compose_literal(v) for v in value]
    return value


def file_hash(path):
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def extract_source(archive, destination, expected_commit=None):
    # A git archive needs only regular files and directories. Reject links and traversal.
    with tarfile.open(archive, "r:gz") as source:
        if expected_commit and source.pax_headers.get("comment") != expected_commit:
            raise DeployError("Git archive commit does not match the requested revision")
        members = source.getmembers()
        for member in members:
            p = Path(member.name)
            if p.is_absolute() or ".." in p.parts or not (member.isfile() or member.isdir()):
                raise DeployError("Source archive contains an unsafe path or file type")
        source.extractall(destination, members=members, filter="data")


def validate_args(args):
    if not re.fullmatch(r"/[A-Za-z0-9_./-]+", args.install_dir) or ".." in Path(args.install_dir).parts:
        raise DeployError("Invalid installation directory")
    if args.install_dir == "/" or not re.fullmatch(r"[a-z0-9][a-z0-9_-]*", args.project_name):
        raise DeployError("Invalid project name or installation directory")
    parsed = urlsplit(args.public_url)
    if (not re.fullmatch(r"https://[A-Za-z0-9.-]+(?::[0-9]{1,5})?/?", args.public_url)
            or parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password
            or parsed.query or parsed.fragment or parsed.path not in ("", "/") or parsed.port == 0):
        raise DeployError("Public URL must be an HTTPS origin without credentials")
    if args.action == "deploy":
        if not args.archive or not re.fullmatch(r"[0-9a-f]{40}", args.commit or "") or not re.fullmatch(r"[0-9a-fA-F]{64}", args.sha256 or ""):
            raise DeployError("Deploy requires an archive, a full Git commit and SHA-256")
        path = Path(args.archive)
        if path.parent != Path("/tmp") or not re.fullmatch(r"canvas-[a-zA-Z0-9-]+\.tar\.gz", path.name):
            raise DeployError("Archive must be a canvas-* upload directly under /tmp")


class Deployment:
    def __init__(self, args):
        self.args = args
        self.install = Path(args.install_dir).resolve()
        self.project = args.project_name
        self.base = ["docker", "compose", "--project-name", self.project,
                     "--project-directory", str(self.install), "--env-file", str(self.install / ".env")]
        self.containers = {}
        self.files = []
        self.config = {}

    def compose(self, *args, files=None, **kwargs):
        command = self.base[:]
        for path in self.files if files is None else files:
            command += ["-f", str(path)]
        return run(command + list(args), **kwargs)

    def discover(self):
        self.containers = {}
        if not (self.install / ".env").is_file():
            raise DeployError("Existing .env not found; this script does not install new servers")
        ids = run(["docker", "ps", "-aq", "--filter", f"label=com.docker.compose.project={self.project}"], capture=True).split()
        if not ids:
            raise DeployError("Existing Compose project not found")
        items = json_run(["docker", "inspect"] + ids)
        for item in items:
            labels = item["Config"].get("Labels") or {}
            if labels.get("com.docker.compose.oneoff", "False").lower() == "true":
                continue
            name = labels.get("com.docker.compose.service")
            if name in self.containers:
                raise DeployError("Scaled/duplicate services are not supported")
            self.containers[name] = item
        if not set(RUNTIME).issubset(self.containers):
            raise DeployError("Expected postgres/redis/yingce-agent/backend/web services")
        labels = self.containers["web"]["Config"]["Labels"]
        if Path(labels["com.docker.compose.project.working_dir"]).resolve() != self.install:
            raise DeployError("Container project directory differs from requested directory")
        self.files = labels["com.docker.compose.project.config_files"].split(",")
        for filename in self.files:
            path = Path(filename).resolve()
            if not path.is_relative_to(self.install) or not path.is_file():
                raise DeployError("Compose input is missing or outside the installation directory")
        # A rollback snapshot is authoritative for restored app containers; unchanged database
        # containers still carry their original file labels. Accept only its recorded inputs.
        permitted_files = set(self.files)
        for filename in self.files:
            path = Path(filename)
            provenance = path.parent / "previous-files.json"
            if path.name == "previous.compose.json" and provenance.is_file():
                original_files = json.loads(provenance.read_text())
                if not all(Path(p).resolve().is_relative_to(self.install) for p in original_files):
                    raise DeployError("Invalid rollback provenance")
                permitted_files.update(original_files)
        # Dependencies may retain labels from an earlier deployment, but not a different config.
        for item in self.containers.values():
            item_labels = item["Config"]["Labels"]
            if Path(item_labels.get("com.docker.compose.project.working_dir", "/")).resolve() != self.install:
                raise DeployError("Mixed container installation directories")
            used = item_labels.get("com.docker.compose.project.config_files", "").split(",")
            if not set(used).issubset(permitted_files):
                raise DeployError("Mixed Compose overrides found; reconcile the deployment first")
        self.config = json.loads(self.compose("config", "--format", "json", capture=True))
        if set(self.config["services"]) != set(RUNTIME) | {"migrate"}:
            raise DeployError("Unexpected Compose services; review before deploying")
        backend_env = self.config["services"]["backend"].get("environment", {})
        if backend_env.get("CANVAS_DATABASE_DRIVER") != "postgres":
            raise DeployError("Only existing PostgreSQL deployments are supported")
        database_url = backend_env.get("DATABASE_URL", "")
        postgres_env = self.config["services"]["postgres"].get("environment", {})
        database = urlsplit(database_url)
        if (database.scheme not in ("postgres", "postgresql") or database.hostname != "postgres"
                or database.port not in (None, 5432)
                or unquote(database.username or "") != postgres_env.get("POSTGRES_USER")
                or unquote(database.path.lstrip('/')) != postgres_env.get("POSTGRES_DB")
                or self.config["services"]["migrate"].get("environment", {}).get("DATABASE_URL") != database_url):
            raise DeployError("Backend/migration must use the project's PostgreSQL database being backed up")
        self.data_source("postgres", "/var/lib/postgresql/data")
        for name in RUNTIME:
            state = self.containers[name]["State"]
            if not state.get("Running") or state.get("Health", {}).get("Status", "healthy") != "healthy":
                raise DeployError(f"Existing service is not healthy: {name}")
        for name, target in [("backend", "/data"), ("web", "/usr/share/nginx/html"), ("redis", "/data")]:
            self.data_source(name, target)

    def data_source(self, name, target):
        matches = [m for m in self.containers[name]["Mounts"] if m["Destination"] == target]
        if len(matches) != 1 or matches[0]["Type"] not in ("volume", "bind"):
            raise DeployError(f"Missing persistent data mount: {name}:{target}")
        path = Path(matches[0]["Source"]).resolve()
        if not path.is_dir() or str(path) in ("/", "/srv", "/var", "/var/lib/docker"):
            raise DeployError(f"Unsafe data mount: {name}")
        return path

    def preflight(self):
        self.discover()
        run(["docker", "compose", "version"], capture=True)
        if shutil.which("tar") is None:
            raise DeployError("tar is required")
        print(f"Project: {self.project}; persistent data and current overrides verified")
        for name in RUNTIME:
            c = self.containers[name]
            print(f"  {name}: {c['Config']['Image']} ({c['State'].get('Health', {}).get('Status', 'running')})")
        print(f"Installation free space: {shutil.disk_usage(self.install).free // 1024**3} GiB")
        print(f"Docker free space: {shutil.disk_usage('/var/lib/docker').free // 1024**3} GiB")
        print("Deploy builds first, then stops app services for a consistent backup and migration.")
        print("Deploy disables the official Host Updater; custom source images are manually managed.")
        print("Preflight is read-only. It does not verify source hotfix parity or business workflows.")

    def snapshot(self, release):
        snapshot = copy.deepcopy(self.config)
        for name, c in self.containers.items():
            if name in snapshot["services"]:
                snapshot["services"][name]["image"] = c["Image"]
                snapshot["services"][name]["pull_policy"] = "never"
                snapshot["services"][name].pop("build", None)
        dump(release / "previous.compose.json", compose_literal(snapshot))
        shutil.copy2(self.install / ".env", release / "previous.env")
        (release / "previous.env").chmod(0o600)
        originals = release / "previous-config"
        originals.mkdir(mode=0o700)
        for index, path in enumerate(self.files):
            shutil.copy2(path, originals / f"{index}-{Path(path).name}")
        dump(release / "previous-files.json", self.files)
        updater = {}
        for key, subcommand in [("active", "is-active"), ("enabled", "is-enabled")]:
            p = subprocess.run(["systemctl", subcommand, UPDATER], capture_output=True, text=True)
            updater[key] = p.stdout.strip()
        dump(release / "updater-before.json", updater)
        dump(release / "mounts-before.json", {n: c["Mounts"] for n, c in self.containers.items()})

    def create_override(self, release, images):
        override = {"services": {name: {"image": images["backend" if name == "migrate" else name], "pull_policy": "never"}
                                 for name in ("backend", "web", "yingce-agent", "migrate")}}
        override["services"]["backend"]["environment"] = {
            "CANVAS_AUTO_MIGRATE": "false", "CANVAS_UPDATER_SOCKET": "", "CANVAS_UPDATER_TOKEN": ""}
        path = release / "compose.source.json"
        dump(path, override)
        return path

    def build(self, source, images, commit):
        version = (source / "VERSION").read_text(encoding="utf-8").strip()
        timestamp = datetime.now(timezone.utc).isoformat()
        for name, dockerfile in [("backend", "backend/Dockerfile"), ("web", "Dockerfile"), ("yingce-agent", "yingce-agent/Dockerfile")]:
            print(f"Building {name} ...", flush=True)
            command = ["docker", "build", "--label", f"org.opencontainers.image.revision={commit}",
                       "--build-arg", f"BUILD_COMMIT={commit}", "--build-arg", f"BUILD_VERSION={version}",
                       "--build-arg", f"BUILD_TIME={timestamp}", "-t", images[name], "-f", str(source / dockerfile)]
            license_key = self.config["services"]["web"].get("build", {}).get("args", {}).get("VITE_TLDRAW_LICENSE_KEY")
            if name == "web" and license_key:
                command += ["--build-arg", f"VITE_TLDRAW_LICENSE_KEY={license_key}"]
            run(command + [str(source)])
            info = json_run(["docker", "image", "inspect", images[name]])[0]
            if info["Config"].get("Labels", {}).get("org.opencontainers.image.revision") != commit:
                raise DeployError("Built image revision does not match requested commit")

    def backup(self, release):
        print("Stopping application services; existing tasks may need up to ten minutes to drain.", flush=True)
        # Keep PostgreSQL online for pg_dump; stop every other writer before file backups.
        self.compose("stop", "web", "backend", "yingce-agent")
        self.compose("stop", "redis")
        pg = self.containers["postgres"]["Id"]
        with open(release / "database.dump", "wb") as stream:
            run(["docker", "exec", pg, "sh", "-ec", 'exec pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"'], stdout=stream)
        with open(release / "database.dump", "rb") as stream:
            p = subprocess.run(["docker", "exec", "-i", pg, "pg_restore", "--list"], stdin=stream, stdout=subprocess.DEVNULL)
            if p.returncode:
                raise DeployError("Database backup failed pg_restore --list validation")
        for name, target in [("backend", "/data"), ("web", "/usr/share/nginx/html"), ("redis", "/data")]:
            run(["tar", "--numeric-owner", "-czf", str(release / f"{name}-data.tar.gz"),
                 "-C", str(self.data_source(name, target)), "."])
        checksums = {p.name: file_hash(p) for p in release.iterdir() if p.is_file() and p.name != "backup-sha256.json"}
        dump(release / "backup-sha256.json", checksums)
        print(f"Consistent backup saved: {release}", flush=True)

    def deploy(self):
        import fcntl
        parent = self.install / "deployment" / "source-releases"
        parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        with open(parent / ".deploy.lock", "w") as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise DeployError("Another source deployment is running") from None
            # Discovery happens again under the lock, not from an earlier preflight snapshot.
            self.preflight()
            archive = Path(self.args.archive)
            if file_hash(archive) != self.args.sha256.lower():
                raise DeployError("Uploaded archive SHA-256 mismatch")
            if shutil.disk_usage('/var/lib/docker').free < 12 * 1024**3:
                raise DeployError("Need at least 12 GiB free on the Docker filesystem for builds")
            release = parent / (datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "-" + self.args.commit[:12] + "-" + uuid.uuid4().hex[:8])
            release.mkdir(mode=0o700)
            source = release / "source"
            source.mkdir(mode=0o700)
            extract_source(archive, source, self.args.commit)
            for filename in ("Dockerfile", "backend/Dockerfile", "yingce-agent/Dockerfile", "VERSION"):
                if not (source / filename).is_file():
                    raise DeployError(f"Required source file missing: {filename}")
            images = {n: f"{self.project}-{n}:source-{self.args.commit}" for n in ("backend", "web", "yingce-agent")}
            self.build(source, images, self.args.commit)
            self.discover()
            self.snapshot(release)
            baseline_config = copy.deepcopy(self.config)
            baseline_ids = {name: c["Id"] for name, c in self.containers.items()}
            new_override = self.create_override(release, images)
            # Retain site-specific and hotfix settings; the last override changes only app images.
            files = self.files + [str(new_override)]
            candidate = json.loads(self.compose("config", "--format", "json", files=files, capture=True))
            for name in RUNTIME:
                for key in ("volumes", "ports", "networks"):
                    if self.config["services"][name].get(key) != candidate["services"][name].get(key):
                        raise DeployError(f"Candidate changes {name} {key}; refusing to touch data or routing")
            sources = [self.data_source(n, t) for n, t in [("backend", "/data"), ("web", "/usr/share/nginx/html"), ("redis", "/data")]]
            estimate = sum(int(run(["du", "-sb", str(p)], capture=True).split()[0]) for p in sources)
            # pg_database_size adds a conservative bound for the logical database dump.
            pg = self.containers["postgres"]["Id"]
            db_size = int(run(["docker", "exec", pg, "sh", "-ec", 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "SELECT pg_database_size(current_database())"'], capture=True).strip())
            if shutil.disk_usage(release).free < (estimate + db_size) * 2 + 1024**3:
                raise DeployError("Insufficient space for a consistent backup with safety margin")
            state = {"commit": self.args.commit, "images": images, "phase": "built", "files": files}
            dump(release / "state.json", state)
            # Keep executable recovery tooling beside the backup; stdin runs are preflight only.
            shutil.copy2(Path(__file__).resolve(), release / "remote-deploy.py")
            print(f"Recovery command: sudo python3 {release}/remote-deploy.py rollback --release-dir {release} --confirm-restore RESTORE-PREDEPLOY-DATA --install-dir {self.install} --project-name {self.project} --public-url {self.args.public_url}", flush=True)
            updater_before = json.loads((release / "updater-before.json").read_text())
            changed = False
            updater_changed = False
            try:
                if updater_before["active"] == "active" or updater_before["enabled"] in ("enabled", "enabled-runtime"):
                    updater_changed = True
                    run(["systemctl", "disable", "--now", UPDATER])
                self.discover()
                if self.config != baseline_config or {name: c["Id"] for name, c in self.containers.items()} != baseline_ids:
                    raise DeployError("Deployment changed while building; rerun preflight before retrying")
                changed = True
                state["phase"] = "stopping"
                dump(release / "state.json", state)
                self.backup(release)
                state["phase"] = "backed-up"
                dump(release / "state.json", state)
                self.compose("up", "-d", "--no-deps", "--wait", "--wait-timeout", "120", "redis", files=files)
                state["phase"] = "migration-started"
                dump(release / "state.json", state)
                self.compose("run", "--rm", "--no-deps", "migrate", files=files)
                self.compose("up", "-d", "--no-deps", "--wait", "--wait-timeout", "180", "yingce-agent", "backend", files=files)
                # Verify readiness through the new backend before opening the web entry point.
                self.compose("exec", "-T", "backend", "wget", "-qO-", "http://127.0.0.1:8080/api/health/ready", files=files, capture=True)
                self.compose("up", "-d", "--no-deps", "--wait", "--wait-timeout", "120", "web", files=files)
                state["phase"] = "running"
                dump(release / "state.json", state)
                try:
                    with urlopen(self.args.public_url.rstrip('/') + "/api/health/ready", timeout=20) as response:
                        if response.status != 200:
                            raise DeployError("Public readiness returned a non-200 status")
                    state["public_check"] = "passed"
                except Exception:
                    state["public_check"] = "failed"
                    print("WARNING: containers are healthy; public HTTPS readiness could not be verified. Check gateway/DNS.")
                dump(release / "state.json", state)
                dump(self.install / "source-deploy-current.json", {"release": str(release), **state})
                print(f"DEPLOY COMPLETE: {self.args.public_url}; commit {self.args.commit}")
                print("Official updater disabled. Validate login, tasks, assets and administrator permissions manually.")
            except BaseException:
                if not changed and updater_changed:
                    restore_updater(updater_before)
                if changed:
                    if state["phase"] in ("stopping", "backed-up"):
                        print("Deployment failed before migration; restarting the previous application.", flush=True)
                        self.compose("up", "-d", "--no-deps", "--wait", "--wait-timeout", "180", "redis", "yingce-agent", "backend", "web")
                        restore_updater(updater_before)
                    else:
                        # Do not put an old binary against a potentially migrated schema.
                        self.compose("stop", "web", "backend", "yingce-agent", files=files)
                        print(f"Deployment failed after migration began. Application stopped; inspect {release} and use explicit rollback.", flush=True)
                raise


def restore_updater(before):
    if before.get("enabled") == "enabled":
        run(["systemctl", "enable", UPDATER])
    elif before.get("enabled") == "enabled-runtime":
        run(["systemctl", "enable", "--runtime", UPDATER])
    if before.get("active") == "active":
        run(["systemctl", "start", UPDATER])


def rollback(args):
    """Explicit destructive restore. Never invoked automatically after a migration."""
    import fcntl
    parent = Path(args.install_dir).resolve() / "deployment" / "source-releases"
    release = Path(args.release_dir or "").resolve()
    if args.confirm_restore != "RESTORE-PREDEPLOY-DATA" or release.parent != parent or not release.is_dir():
        raise DeployError("Rollback needs a release directory and --confirm-restore RESTORE-PREDEPLOY-DATA")
    with open(parent / ".deploy.lock", "w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise DeployError("Another deployment/rollback is running") from None
        checksums = json.loads((release / "backup-sha256.json").read_text())
        for name in ["database.dump", "backend-data.tar.gz", "web-data.tar.gz", "redis-data.tar.gz", "previous.env", "previous.compose.json", "mounts-before.json", "updater-before.json"]:
            if name not in checksums or file_hash(release / name) != checksums[name]:
                raise DeployError("Rollback backup is incomplete or SHA-256 verification failed")
        d = Deployment(args)
        d.files = [str(release / "previous.compose.json")]
        # Use saved environment, never current credentials to interpret the saved configuration.
        d.base[-1] = str(release / "previous.env")
        config = json.loads(d.compose("config", "--format", "json", capture=True))
        for service in config["services"].values():
            run(["docker", "image", "inspect", service["image"]], capture=True)
        mounts = json.loads((release / "mounts-before.json").read_text())
        live_ids = run(["docker", "ps", "-aq", "--filter", f"label=com.docker.compose.project={args.project_name}"], capture=True).split()
        live = {c["Config"].get("Labels", {}).get("com.docker.compose.service"): c for c in json_run(["docker", "inspect"] + live_ids)} if live_ids else {}
        plans = []
        # Stage every archive before stopping or moving current data. Keep originals until the
        # operator removes them after business verification; no rollback path deletes live data.
        for name, target in [("backend", "/data"), ("web", "/usr/share/nginx/html"), ("redis", "/data")]:
            candidates = [m for m in mounts[name] if m["Destination"] == target]
            current = [m for m in live.get(name, {}).get("Mounts", []) if m["Destination"] == target]
            if len(candidates) != 1 or len(current) != 1 or candidates[0]["Source"] != current[0]["Source"]:
                raise DeployError("Saved and current data mounts differ; manual recovery required")
            destination = Path(candidates[0]["Source"]).resolve()
            if not destination.is_dir() or len(destination.parts) < 4 or destination in (Path('/srv/apps'), Path('/var/lib/docker')):
                raise DeployError("Unsafe rollback data path")
            archive = release / f"{name}-data.tar.gz"
            with tarfile.open(archive, "r:gz") as stream:
                members = stream.getmembers()
                for member in members:
                    if Path(member.name).is_absolute() or ".." in Path(member.name).parts:
                        raise DeployError("Unsafe backup archive member")
                    if member.islnk() or member.issym():
                        if Path(member.linkname).is_absolute() or ".." in Path(member.linkname).parts:
                            raise DeployError("Backup contains an external link; manual recovery required")
                    elif not (member.isfile() or member.isdir()):
                        raise DeployError("Unsupported special file in backup")
                needed = sum(m.size for m in members if m.isfile())
            if shutil.disk_usage(destination).free < needed * 2 + 1024**3:
                raise DeployError("Insufficient space to stage rollback while preserving current data")
            staging = Path(tempfile.mkdtemp(prefix=".canvas-restore-", dir=destination.parent))
            incoming = staging / "incoming"
            previous = staging / "previous"
            incoming.mkdir(mode=0o700)
            previous.mkdir(mode=0o700)
            run(["tar", "--numeric-owner", "-xzf", str(archive), "-C", str(incoming)])
            plans.append({"service": name, "destination": str(destination), "incoming": str(incoming), "previous": str(previous)})
        attempt = release / ("rollback-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "-" + uuid.uuid4().hex[:8])
        attempt.mkdir(mode=0o700)
        dump(attempt / "file-recovery-plan.json", plans)
        active = subprocess.run(["systemctl", "is-active", "--quiet", UPDATER]).returncode == 0
        enabled = subprocess.run(["systemctl", "is-enabled", "--quiet", UPDATER]).returncode == 0
        if active or enabled:
            run(["systemctl", "disable", "--now", UPDATER])
        try:
            d.compose("stop", "web", "backend", "yingce-agent", "redis")
            pg_ids = d.compose("ps", "-q", "postgres", capture=True).split()
            if len(pg_ids) != 1:
                raise DeployError("PostgreSQL must be running for restore")
            with open(attempt / "database-before-rollback.dump", "wb") as stream:
                run(["docker", "exec", pg_ids[0], "sh", "-ec", 'exec pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB"'], stdout=stream)
            with open(release / "database.dump", "rb") as stream:
                if subprocess.run(["docker", "exec", "-i", pg_ids[0], "pg_restore", "--list"], stdin=stream, stdout=subprocess.DEVNULL).returncode:
                    raise DeployError("Database dump failed restore precheck")
            for plan in plans:
                destination = Path(plan["destination"])
                previous = Path(plan["previous"])
                print(f"Restoring {plan['service']}; preserving current files at {previous}", flush=True)
                for child in destination.iterdir():
                    child.rename(previous / child.name)
                for child in Path(plan["incoming"]).iterdir():
                    child.rename(destination / child.name)
            with open(release / "database.dump", "rb") as stream:
                command = ["docker", "exec", "-i", pg_ids[0], "sh", "-ec", 'exec pg_restore --clean --if-exists --exit-on-error --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"']
                if subprocess.run(command, stdin=stream).returncode:
                    raise DeployError("Database restore failed; keep application stopped and inspect the backup")
            d.compose("up", "-d", "--no-deps", "--wait", "--wait-timeout", "180", "redis", "yingce-agent", "backend", "web")
            restore_updater(json.loads((release / "updater-before.json").read_text()))
        except BaseException:
            d.compose("stop", "web", "backend", "yingce-agent")
            print(f"Rollback incomplete. Application and updater remain stopped. Current file copies and database are recorded at {attempt}.", flush=True)
            raise
        dump(Path(args.install_dir) / "source-deploy-current.json", {"phase": "rolled-back", "release": str(release), "recovery_copy": str(attempt)})
        print("ROLLBACK COMPLETE. Live data is restored to the selected backup; newer data remains in recovery copies.")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("preflight", "status", "logs", "deploy", "rollback"))
    parser.add_argument("--install-dir", required=True)
    parser.add_argument("--project-name", required=True)
    parser.add_argument("--public-url", required=True)
    parser.add_argument("--archive")
    parser.add_argument("--sha256")
    parser.add_argument("--commit")
    parser.add_argument("--release-dir")
    parser.add_argument("--confirm-restore")
    args = parser.parse_args(argv)
    validate_args(args)
    if sys.version_info < (3, 12):
        raise DeployError("Python 3.12 or newer is required")
    if sys.platform != "linux" or os.geteuid() != 0:
        raise DeployError("Run on the Linux server with sudo -n python3")
    os.umask(0o077)
    if args.action == "rollback":
        rollback(args)
        return
    d = Deployment(args)
    if args.action == "deploy":
        d.deploy()
    elif args.action == "preflight":
        d.preflight()
    else:
        # Status/logs remain useful if a service is unhealthy; do not use healthy-only discovery.
        ids = run(["docker", "ps", "-aq", "--filter", f"label=com.docker.compose.project={args.project_name}"], capture=True).split()
        if not ids:
            raise DeployError("Compose project not found")
        if args.action == "status":
            run(["docker", "ps", "-a", "--filter", f"label=com.docker.compose.project={args.project_name}"])
        else:
            for item in json_run(["docker", "inspect"] + ids):
                if item["Config"].get("Labels", {}).get("com.docker.compose.service") == "backend":
                    run(["docker", "logs", "--tail", "100", item["Id"]])


if __name__ == "__main__":
    try:
        main()
    except (DeployError, OSError, ValueError, KeyError, tarfile.TarError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        sys.exit(1)

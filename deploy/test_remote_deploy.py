"""Safety regression tests; no Docker daemon or server access is required."""
import argparse
import contextlib
import copy
import importlib.util
import io
import json
from pathlib import Path, PurePosixPath
import sys
import tarfile
import tempfile
import types
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("remote_deploy", Path(__file__).with_name("remote-deploy.py"))
remote = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(remote)


def arguments(**changes):
    values = dict(action="preflight", install_dir="/srv/apps/open-ai-canvas",
                  project_name="open-ai-canvas", public_url="https://example.com",
                  archive=None, commit=None, sha256=None, release_dir=None,
                  confirm_restore=None)
    values.update(changes)
    return argparse.Namespace(**values)


class ArchiveTests(unittest.TestCase):
    def make_archive(self, root, name, kind=tarfile.REGTYPE):
        archive = root / "source.tar.gz"
        with tarfile.open(archive, "w:gz") as stream:
            member = tarfile.TarInfo(name)
            member.type = kind
            if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE):
                member.linkname = "../../outside"
            stream.addfile(member)
        return archive

    def test_rejects_escaping_paths_and_links_before_extraction(self):
        for name, kind in [("../outside", tarfile.REGTYPE),
                           ("safe/../../outside", tarfile.REGTYPE),
                           ("/absolute", tarfile.REGTYPE),
                           ("link", tarfile.SYMTYPE), ("hard", tarfile.LNKTYPE),
                           ("fifo", tarfile.FIFOTYPE)]:
            with self.subTest(name=name, kind=kind), tempfile.TemporaryDirectory() as folder:
                root = Path(folder)
                archive = self.make_archive(root, name, kind)
                # Production runs on Linux; retain its path semantics on Windows CI.
                with mock.patch.object(remote, "Path", PurePosixPath):
                    with self.assertRaises(remote.DeployError):
                        remote.extract_source(archive, root / "out")
                self.assertFalse((root / "out").exists())

    def test_regular_git_archive_extracts(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            archive = self.make_archive(root, "backend/main.go")
            remote.extract_source(archive, root / "out")
            self.assertTrue((root / "out/backend/main.go").is_file())


class ArgumentTests(unittest.TestCase):
    def validate(self, **changes):
        with mock.patch.object(remote, "Path", PurePosixPath):
            remote.validate_args(arguments(**changes))

    def test_accepts_https_origin_and_full_deploy_identifiers(self):
        self.validate()
        self.validate(action="deploy", archive="/tmp/canvas-abc-123.tar.gz",
                      commit="a" * 40, sha256="B" * 64)

    def test_rejects_dangerous_installations_projects_and_origins(self):
        cases = [dict(install_dir=p) for p in ("/", "/srv/../etc", "relative", "/srv/app;id", "/srv/app name")]
        cases += [dict(project_name=p) for p in ("-option", "name;id", "name space", "../other")]
        cases += [dict(public_url=p) for p in ("http://example.com", "https://user:password@example.com", "https://example.com/path", "https://example.com?key=value", "https://example.com#fragment", "https://")]
        for case in cases:
            with self.subTest(case=case), self.assertRaises(remote.DeployError):
                self.validate(**case)

    def test_deploy_requires_hash_commit_and_safe_upload_path(self):
        valid = dict(action="deploy", archive="/tmp/canvas-abc.tar.gz", commit="a" * 40, sha256="b" * 64)
        for changes in (dict(sha256=None), dict(sha256="xyz"), dict(commit="abc"),
                        dict(archive=None), dict(archive="/etc/canvas-abc.tar.gz"),
                        dict(archive="/tmp/nested/canvas-abc.tar.gz")):
            case = {**valid, **changes}
            with self.subTest(case=changes), self.assertRaises(remote.DeployError):
                self.validate(**case)


class ExistingDeploymentTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / ".env").write_text("SECRET=do-not-print$token\n", encoding="utf-8")
        self.base = self.root / "docker-compose.deploy.yml"
        self.hotfix = self.root / "hotfix/compose.override.yml"
        self.hotfix.parent.mkdir()
        self.base.write_text("services: {}", encoding="utf-8")
        self.hotfix.write_text("services: {}", encoding="utf-8")
        self.files = [str(self.base), str(self.hotfix)]
        self.args = arguments(install_dir=str(self.root))
        self.deployment = remote.Deployment(self.args)
        self.config = {"services": {name: {"image": "mutable:latest"} for name in (*remote.RUNTIME, "migrate")}}
        self.config["services"]["backend"]["environment"] = {
            "CANVAS_DATABASE_DRIVER": "postgres", "SECRET": "secret$value${OTHER}",
            "DATABASE_URL": "postgresql://canvas:secret@postgres:5432/canvas"}
        self.config["services"]["migrate"]["environment"] = {"DATABASE_URL": "postgresql://canvas:secret@postgres:5432/canvas"}
        self.config["services"]["postgres"]["environment"] = {"POSTGRES_USER": "canvas", "POSTGRES_DB": "canvas"}
        self.items = []
        for name in (*remote.RUNTIME, "migrate"):
            source = self.root / (name + "-data")
            source.mkdir()
            target = "/usr/share/nginx/html" if name == "web" else ("/var/lib/postgresql/data" if name == "postgres" else "/data")
            self.items.append({"Id": name + "-container", "Image": "sha256:" + name,
                               "Config": {"Image": "mutable:latest", "Labels": {
                                   "com.docker.compose.service": name,
                                   "com.docker.compose.project.working_dir": str(self.root),
                                   "com.docker.compose.project.config_files": ",".join(self.files if name in ("backend", "web") else self.files[:1])}},
                               "State": {"Running": name != "migrate", "Health": {"Status": "healthy"}},
                               "Mounts": [{"Type": "volume", "Source": str(source), "Destination": target}]})
        self.run = mock.patch.object(remote, "run", return_value="id1 id2").start()
        self.json_run = mock.patch.object(remote, "json_run", side_effect=lambda command: copy.deepcopy(self.items)).start()
        self.compose = mock.patch.object(self.deployment, "compose", side_effect=lambda *a, **kw: json.dumps(self.config)).start()
        self.addCleanup(mock.patch.stopall)

    def test_discovers_six_services_with_old_hotfix_on_only_app_containers(self):
        self.deployment.discover()
        self.assertEqual(set(self.deployment.containers), set(remote.RUNTIME) | {"migrate"})
        self.assertEqual(self.deployment.files, self.files)
        self.assertEqual(self.deployment.data_source("backend", "/data"), self.root / "backend-data")

    def test_refuses_missing_mount_extra_service_and_unhealthy_runtime(self):
        for problem in ("mount", "extra", "unhealthy", "duplicate", "foreign_override"):
            with self.subTest(problem=problem):
                saved_items, saved_config = copy.deepcopy(self.items), copy.deepcopy(self.config)
                if problem == "mount":
                    next(c for c in self.items if c["Config"]["Labels"]["com.docker.compose.service"] == "backend")["Mounts"] = []
                elif problem == "extra":
                    self.config["services"]["unexpected"] = {"image": "other"}
                elif problem == "unhealthy":
                    self.items[0]["State"]["Health"]["Status"] = "unhealthy"
                elif problem == "duplicate":
                    self.items.append(copy.deepcopy(self.items[0]))
                else:
                    self.items[0]["Config"]["Labels"]["com.docker.compose.project.config_files"] = str(self.root / "unknown.yml")
                with self.assertRaises(remote.DeployError):
                    self.deployment.discover()
                self.items, self.config = saved_items, saved_config

    def test_refuses_external_or_mismatched_database_before_any_deploy(self):
        for service in ("backend", "migrate"):
            original = self.config["services"][service]["environment"]["DATABASE_URL"]
            with self.subTest(service=service):
                self.config["services"][service]["environment"]["DATABASE_URL"] = "postgresql://canvas:secret@external-db:5432/canvas"
                with self.assertRaisesRegex(remote.DeployError, "PostgreSQL database"):
                    self.deployment.discover()
                self.config["services"][service]["environment"]["DATABASE_URL"] = original

    def test_snapshot_pins_actual_image_ids_and_keeps_secrets_off_console(self):
        self.deployment.discover()
        release = self.root / "release"
        release.mkdir()
        output = io.StringIO()
        with mock.patch.object(remote.subprocess, "run", return_value=types.SimpleNamespace(stdout="active\n", returncode=0)), contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
            self.deployment.snapshot(release)
        saved = json.loads((release / "previous.compose.json").read_text(encoding="utf-8"))
        for name, service in saved["services"].items():
            self.assertEqual(service["image"], "sha256:" + name)
            self.assertEqual(service["pull_policy"], "never")
        self.assertEqual(saved["services"]["backend"]["environment"]["SECRET"], "secret$$value$${OTHER}")
        self.assertEqual((release / "previous.env").read_text(), "SECRET=do-not-print$token\n")
        self.assertNotIn("secret", output.getvalue().lower())
        self.assertNotIn("token", output.getvalue())


class LiteralTests(unittest.TestCase):
    def test_recursively_preserves_dollars_without_mutating_input(self):
        value = {"environment": {"PASSWORD": "a$b${C}$$d"}, "command": ["sh", "echo $HOME", 7, None, True]}
        before = copy.deepcopy(value)
        escaped = remote.compose_literal(value)
        self.assertEqual(escaped["environment"]["PASSWORD"], "a$$b$${C}$$$$d")
        self.assertEqual(escaped["command"], ["sh", "echo $$HOME", 7, None, True])
        self.assertEqual(value, before)


class RollbackGuardsTests(unittest.TestCase):
    def test_failed_confirmation_or_checksum_never_stops_services_or_deletes_data(self):
        fake_fcntl = types.SimpleNamespace(flock=mock.Mock(), LOCK_EX=1, LOCK_NB=2)
        for problem in ("confirmation", "hash", "missing_checksum"):
            with self.subTest(problem=problem), tempfile.TemporaryDirectory() as folder:
                install = Path(folder)
                release = install / "deployment/source-releases/release-one"
                release.mkdir(parents=True)
                (release / "database.dump").write_bytes(b"database")
                checksums = {} if problem == "missing_checksum" else {"database.dump": "0" * 64}
                (release / "backup-sha256.json").write_text(json.dumps(checksums))
                sentinel = install / "valuable-data"
                sentinel.write_bytes(b"keep")
                args = arguments(install_dir=str(install), release_dir=str(release),
                                 confirm_restore="WRONG" if problem == "confirmation" else "RESTORE-PREDEPLOY-DATA")
                with mock.patch.dict(sys.modules, {"fcntl": fake_fcntl}), mock.patch.object(remote, "run") as command, mock.patch.object(remote, "Deployment") as deployment, mock.patch.object(remote.shutil, "rmtree") as remove_tree, mock.patch.object(remote.Path, "unlink") as unlink:
                    with self.assertRaises(remote.DeployError):
                        remote.rollback(args)
                    command.assert_not_called()
                    deployment.assert_not_called()
                    remove_tree.assert_not_called()
                    unlink.assert_not_called()
                    self.assertEqual(sentinel.read_bytes(), b"keep")


class MigrationFailureTests(unittest.TestCase):
    def test_failure_after_migration_begins_stops_app_without_starting_old_binary(self):
        fake_fcntl = types.SimpleNamespace(flock=mock.Mock(), LOCK_EX=1, LOCK_NB=2)
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            archive = root / "source.tar.gz"
            archive.write_bytes(b"verified-upload")
            args = arguments(action="deploy", install_dir=str(root), archive=str(archive),
                             commit="a" * 40, sha256=remote.file_hash(archive))
            deployment = remote.Deployment(args)
            deployment.config = {"services": {name: {} for name in (*remote.RUNTIME, "migrate")}}
            deployment.containers = {name: {"Id": name} for name in (*remote.RUNTIME, "migrate")}
            deployment.files = [str(root / "existing.yml")]
            calls = []

            def extract(upload, source, expected_commit=None):
                for name in ("Dockerfile", "backend/Dockerfile", "yingce-agent/Dockerfile", "VERSION"):
                    path = source / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text("fixture")

            def snapshot(release):
                remote.dump(release / "updater-before.json", {"active": "inactive", "enabled": "disabled"})

            def compose(*command, **options):
                calls.append((command, options))
                if command[:1] == ("config",):
                    return json.dumps(deployment.config)
                if command[:1] == ("run",):
                    raise remote.DeployError("Simulated failed migration")
                return ""

            with mock.patch.dict(sys.modules, {"fcntl": fake_fcntl}), \
                 mock.patch.object(deployment, "preflight"), \
                 mock.patch.object(deployment, "discover"), \
                 mock.patch.object(deployment, "build"), \
                 mock.patch.object(deployment, "snapshot", side_effect=snapshot), \
                 mock.patch.object(deployment, "backup"), \
                 mock.patch.object(deployment, "data_source", return_value=root), \
                 mock.patch.object(deployment, "compose", side_effect=compose), \
                 mock.patch.object(remote, "extract_source", side_effect=extract), \
                 mock.patch.object(remote, "run", return_value="0"), \
                 mock.patch.object(remote.shutil, "disk_usage", return_value=types.SimpleNamespace(free=100 * 1024**3)), \
                 mock.patch.object(remote, "restore_updater") as updater, \
                 contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(remote.DeployError, "failed migration"):
                    deployment.deploy()
            migration = next(index for index, (command, _) in enumerate(calls) if command[0] == "run")
            self.assertEqual(calls[migration + 1][0], ("stop", "web", "backend", "yingce-agent"))
            self.assertFalse(any(command[0] == "up" for command, _ in calls[migration + 1:]))
            updater.assert_not_called()
            releases = list((root / "deployment/source-releases").glob("*/state.json"))
            self.assertEqual(len(releases), 1)
            self.assertEqual(json.loads(releases[0].read_text())["phase"], "migration-started")


if __name__ == "__main__":
    unittest.main()

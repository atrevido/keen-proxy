"""Exercise release installation without network or a live router."""
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class BootstrapFixture(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.prefix = self.root / 'opt'
        self.commands = self.root / 'commands'
        self.commands.mkdir()
        self.mirror = self.root / 'mirror'
        self.mirror.mkdir()
        self.requests = self.root / 'requests'
        self.env = dict(os.environ, PATH=f'{self.commands}:{os.environ["PATH"]}',
                        KEENPROXY_PREFIX=str(self.prefix), FIXTURES=str(self.mirror),
                        REQUESTS=str(self.requests), CPU='aarch64')
        self.command('uname', 'echo "$CPU"')
        self.command('pidof', 'exit 1')
        self.command('curl', '''
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) out=$2; shift ;;
        https://*) url=$1 ;;
    esac
    shift
done
printf '%s\\n' "$url" >> "$REQUESTS"
case "$url" in
    */main/version.txt) path=version.txt ;;
    */releases/download/*) path=${url#*/releases/download/} ;;
    *) exit 22 ;;
esac
cp "$FIXTURES/$path" "$out"
''')
        for path in ('bin', 'sbin', 'etc/init.d'):
            (self.prefix / path).mkdir(parents=True)
        for name in ('iptables', 'iptables-restore'):
            self.executable(self.prefix / 'sbin' / name, 'exit 0')
        self.executable(self.prefix / 'etc/init.d/rc.unslung', 'exit 0')
        (self.mirror / 'version.txt').write_text('1.2.3\n')

    def executable(self, path, script):
        path.write_text('#!/bin/sh\n' + script + '\n')
        path.chmod(0o755)

    def command(self, name, script):
        self.executable(self.commands / name, script)

    def release(self, tag, arch='arm64', lite=False, start=True, dry_run='exit 0',
                stop='rm -f "$state"'):
        folder = self.mirror / tag
        folder.mkdir()
        asset = f'keenproxy-{"lite-" if lite else ""}linux-{arch}'
        (folder / asset).write_text(
            f'#!/bin/sh\n# {tag}\nif [ "$1" = --fixture-start ]; then exit {0 if start else 1}; fi\n{dry_run}\n')
        (folder / 'install.sh').write_bytes((ROOT / 'tools/install.sh').read_bytes())
        self.executable(folder / 'S99keenproxy', f'''# KeenProxy Entware service
state="$KEENPROXY_PREFIX/running"
test -n "${{KEENPROXY_SERVICE_LOCK_TOKEN:-}}" || exit 1
case "$1" in
    start) "$KEENPROXY_PREFIX/bin/keenproxy" --fixture-start && touch "$state" ;;
    check) test -f "$state" ;;
    stop) {stop} ;;
    *) exit 2 ;;
esac''')
        self.write_checksums(folder)
        return folder / asset

    def write_checksums(self, folder):
        (folder / 'SHA256SUMS').write_text(''.join(
            f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n'
            for p in sorted(folder.iterdir()) if p.name != 'SHA256SUMS'))

    def install(self, *args):
        return self.bootstrap('--without-trusttunnel', *args)

    def bootstrap(self, *args):
        with (ROOT / 'tools/bootstrap.sh').open('rb') as script:
            return subprocess.run(['sh', '-s', '--', *args],
                                  stdin=script, env=self.env, capture_output=True,
                                  text=True, timeout=20)

    @property
    def binary(self):
        return self.prefix / 'bin/keenproxy'

    @property
    def config(self):
        return self.prefix / 'etc/keenproxy/config.yaml'

    @property
    def service(self):
        return self.prefix / 'etc/init.d/S99keenproxy'


class BootstrapTest(BootstrapFixture):
    def test_uninstall_stops_service_and_preserves_settings_without_network(self):
        self.release('v1.2.3')
        self.assertEqual(self.install().returncode, 0)
        token = self.config.with_name('token')
        token.write_text('test token\n')
        self.config.write_text('schema_version: 1\n# local settings\n')
        client = self.prefix / 'bin/trusttunnel_client'
        self.executable(client, 'exit 0')
        before = self.requests.read_bytes()
        self.env['CPU'] = 'unsupported'
        for _ in range(2):
            result = self.bootstrap('--uninstall')
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse((self.prefix / 'running').exists())
            self.assertFalse(self.binary.exists())
            self.assertFalse(self.service.exists())
            self.assertEqual(token.read_text(), 'test token\n')
            self.assertIn('# local settings', self.config.read_text())
            self.assertTrue(client.exists())
        self.assertEqual(self.requests.read_bytes(), before)

    def test_uninstall_refuses_an_unmanaged_binary(self):
        self.executable(self.binary, 'echo old')
        result = self.bootstrap('--uninstall')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('unmanaged', result.stderr)
        self.assertIn('echo old', self.binary.read_text())
        self.assertFalse(self.requests.exists())

    def test_uninstall_preserves_installation_when_stop_fails(self):
        binary = self.release('v1.2.3', stop='exit 1')
        self.assertEqual(self.install().returncode, 0)
        result = self.bootstrap('--uninstall')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('did not stop', result.stderr)
        self.assertEqual(self.binary.read_bytes(), binary.read_bytes())
        self.assertTrue(self.service.exists())
        self.assertTrue((self.prefix / 'running').exists())

    def test_uninstall_rolls_back_a_partial_removal(self):
        binary = self.release('v1.2.3')
        self.assertEqual(self.install().returncode, 0)
        service = self.service.read_bytes()
        self.command('rm', """
for path in "$@"; do
    [ "$path" != "$KEENPROXY_PREFIX/etc/init.d/S99keenproxy" ] || exit 1
done
exec /bin/rm "$@"
""")
        result = self.bootstrap('--uninstall')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('restoring the previous installation', result.stderr)
        self.assertEqual(self.binary.read_bytes(), binary.read_bytes())
        self.assertEqual(self.service.read_bytes(), service)
        self.assertTrue((self.prefix / 'running').exists())

    def test_bootstrap_installs_starts_and_preserves_configuration_on_update(self):
        first = self.release('v1.2.3')
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.binary.read_bytes(), first.read_bytes())
        self.assertEqual(self.config.stat().st_mode & 0o777, 0o600)
        self.assertTrue((self.prefix / 'running').exists())
        self.config.write_text('schema_version: 1\n# user setting\n')
        token = self.config.with_name('token')
        token.write_text('test fixture\n')
        next_release = self.release('v1.2.4')
        (self.mirror / 'version.txt').write_text('1.2.4\n')
        for _ in range(2):
            result = self.install()
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.binary.read_bytes(), next_release.read_bytes())
        self.assertIn('# user setting', self.config.read_text())
        self.assertEqual(token.read_text(), 'test fixture\n')

    def test_explicit_lite_version_skips_main(self):
        self.env['CPU'] = 'mipsel'
        release = self.release('v0.9.0', arch='mipsle', lite=True)
        result = self.install('--version', '0.9.0', '--lite')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.binary.read_bytes(), release.read_bytes())
        self.assertNotIn('/main/version.txt', self.requests.read_text())

    def test_bad_checksum_keeps_running_release(self):
        first = self.release('v1.2.3')
        self.assertEqual(self.install().returncode, 0)
        corrupt = self.release('v1.2.4')
        corrupt.write_text('bad download')
        result = self.install('--version', 'v1.2.4')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('SHA256', result.stderr)
        self.assertEqual(self.binary.read_bytes(), first.read_bytes())
        self.assertTrue((self.prefix / 'running').exists())

    def test_failed_start_restores_previous_release(self):
        first = self.release('v1.2.3')
        self.assertEqual(self.install().returncode, 0)
        self.release('v1.2.4', start=False)
        result = self.install('--version', 'v1.2.4')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.binary.read_bytes(), first.read_bytes())
        self.assertTrue((self.prefix / 'running').exists())

    def test_missing_assets_and_unknown_cpu_leave_existing_files(self):
        first = self.release('v1.2.3')
        self.assertEqual(self.install().returncode, 0)
        self.assertNotEqual(self.install('--version', 'v1.2.4').returncode, 0)
        self.env['CPU'] = 'armv7l'
        self.assertNotEqual(self.install().returncode, 0)
        self.assertEqual(self.binary.read_bytes(), first.read_bytes())
        self.assertTrue((self.prefix / 'running').exists())

    def test_unmanaged_process_is_left_alone(self):
        self.release('v1.2.3')
        self.executable(self.binary, 'echo old')
        self.command('pidof', 'echo 1234')
        result = self.install()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('unmanaged', result.stderr)
        self.assertIn('echo old', self.binary.read_text())
        self.assertFalse(self.config.exists())

    def test_dry_run_does_not_print_credentials(self):
        self.release('v1.2.3', dry_run='echo synthetic-password >&2; exit 1')
        result = self.install()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('failed validation', result.stderr)
        self.assertNotIn('synthetic-password', result.stdout + result.stderr)

    def test_upgrade_refuses_to_replace_files_during_service_action(self):
        first = self.release('v1.2.3')
        self.assertEqual(self.install().returncode, 0)
        self.release('v1.2.4')
        lock = self.prefix / 'var/run/keenproxy/action.lock'
        lock.mkdir(parents=True)
        result = self.install('--version', 'v1.2.4')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('service action already in progress', result.stderr)
        self.assertEqual(self.binary.read_bytes(), first.read_bytes())
        self.assertTrue((self.prefix / 'running').exists())
        self.assertTrue(lock.is_dir())

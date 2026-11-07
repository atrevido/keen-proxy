"""Exercise the actual Entware controller with isolated Linux processes."""
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import unittest

from test_bootstrap import BootstrapFixture, ROOT

CONTROLLER = ROOT / 'tools/S99keenproxy'


@unittest.skipUnless(sys.platform == 'linux', 'requires Linux /proc/PID/exe')
class ServiceTest(BootstrapFixture):
    @classmethod
    def setUpClass(cls):
        build = tempfile.TemporaryDirectory()
        cls.addClassCleanup(build.cleanup)
        source = Path(__file__).parent / 'fixtures/service_daemon.c'
        cls.daemon = Path(build.name) / 'daemon'
        cls.broken_daemon = Path(build.name) / 'broken-daemon'
        for binary, flags in [(cls.daemon, []), (cls.broken_daemon, ['-DFAIL_START'])]:
            subprocess.run(['cc', *flags, str(source), '-o', str(binary)],
                           check=True, timeout=30)

    def setUp(self):
        super().setUp()
        self.env.pop('KEENPROXY_SERVICE_LOCK_TOKEN', None)
        self.pids = self.root / 'daemon-pids'
        self.pids.touch()
        self.env.update(FIXTURE_PIDS=str(self.pids), FIXTURE_READY='yes')
        self.addCleanup(self.stop_fixture_processes)
        self.state = self.prefix / 'var/run/keenproxy'
        self.pidfile = self.state / 'pid'
        self.lock = self.state / 'action.lock'
        shutil.copy2(self.daemon, self.binary)
        # Keep real sleeps for process scheduling but shorten readiness timeouts.
        self.command('sleep', 'exec /bin/sleep 0.1')
        self.executable(self.prefix / 'sbin/iptables', '''
case "$*" in
    "-t nat -S") exit 0 ;;
    "-t nat -C PREROUTING -j KEENPROXY_DNS") [ "$FIXTURE_READY" = yes ] ;;
    *) exit 1 ;;
esac''')

    def stop_fixture_processes(self):
        """Reclaim fixture processes even if a failed assertion leaves no pidfile."""
        for line in self.pids.read_text().splitlines():
            pid = int(line)
            executable = self.process_executable(pid)
            if executable in (str(self.binary), f'{self.binary} (deleted)'):
                try:
                    os.kill(pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass

    @staticmethod
    def process_executable(pid):
        try:
            return os.readlink(f'/proc/{pid}/exe')
        except FileNotFoundError:
            return None

    def action(self, name, *, controller=CONTROLLER, token=None):
        env = self.env.copy()
        if token is not None:
            env['KEENPROXY_SERVICE_LOCK_TOKEN'] = token
        return subprocess.run(['sh', str(controller), name], env=env,
                              capture_output=True, text=True, timeout=15)

    def assert_success(self, result):
        self.assertEqual(result.returncode, 0, result.stderr)

    def running_pid(self):
        pid = int(self.pidfile.read_text())
        self.assertEqual(self.process_executable(pid), str(self.binary))
        return pid

    def test_start_is_idempotent_and_restart_replaces_the_process(self):
        self.assert_success(self.action('start'))
        first = self.running_pid()
        self.assert_success(self.action('start'))
        self.assertEqual(self.running_pid(), first)
        self.assert_success(self.action('restart'))
        second = self.running_pid()
        self.assertNotEqual(first, second)
        self.assertIsNone(self.process_executable(first))
        self.assert_success(self.action('stop'))
        self.assertIsNone(self.process_executable(second))
        self.assertFalse(self.pidfile.exists())
        self.assertNotEqual(self.action('status').returncode, 0)
        self.assertFalse(self.lock.exists())

    def test_foreign_pid_is_never_treated_as_the_managed_process(self):
        with subprocess.Popen(['/bin/sleep', '30']) as other:
            try:
                self.state.mkdir(parents=True)
                self.pidfile.write_text(f'{other.pid}\n')
                self.assertNotEqual(self.action('check').returncode, 0)
                self.command('pidof', f'echo {other.pid}')
                self.assertIn('unmanaged process', self.action('start').stderr)
                self.assert_success(self.action('stop'))
                self.assertIsNone(other.poll())
                self.assertFalse(self.pidfile.exists())
            finally:
                other.terminate()

    def test_only_the_installer_lock_owner_can_delegate_actions(self):
        self.lock.mkdir(parents=True)
        owner = self.lock / 'owner'
        owner.write_text('installer\n')
        self.assertIn('already in progress', self.action('start').stderr)
        self.assertIn('invalid installer lock', self.action('start', token='wrong').stderr)
        self.assertFalse(self.pidfile.exists())
        self.assert_success(self.action('start', token='installer'))
        pid = self.running_pid()
        self.assert_success(self.action('stop', token='installer'))
        self.assertIsNone(self.process_executable(pid))
        self.assertEqual(owner.read_text(), 'installer\n')

    def test_failed_firewall_readiness_stops_the_daemon(self):
        self.env['FIXTURE_READY'] = 'no'
        result = self.action('start')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('startup failed', result.stderr)
        pids = self.pids.read_text().splitlines()
        self.assertEqual(len(pids), 1)
        self.assertIsNone(self.process_executable(pids[0]))
        self.assertFalse(self.pidfile.exists())
        self.assertFalse(self.lock.exists())

    def publish(self, tag, daemon):
        asset = self.release(tag)
        shutil.copyfile(daemon, asset)
        shutil.copy2(CONTROLLER, asset.with_name('S99keenproxy'))
        self.write_checksums(asset.parent)

    def test_failed_upgrade_restores_and_restarts_the_previous_release(self):
        self.binary.unlink()
        self.publish('v1.2.3', self.daemon)
        self.assert_success(self.install())
        original = self.running_pid()
        self.publish('v1.2.4', self.broken_daemon)
        result = self.install('--version', 'v1.2.4')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('new service failed to start', result.stderr)
        self.assertIn('restoring the previous installation', result.stderr)
        self.assertEqual(self.binary.read_bytes(), self.daemon.read_bytes())
        self.assertEqual(self.service.read_bytes(), CONTROLLER.read_bytes())
        self.assert_success(self.action('status', controller=self.service))
        self.assertNotEqual(self.running_pid(), original)
        self.assertIsNone(self.process_executable(original))
        self.assertFalse(self.lock.exists())

"""Run the installer in an isolated prefix; fake only the network/CPU boundary."""
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[2] / 'tools/install.sh'
DIGEST = '880e9c8afd5cce7ff38c53978f0ea530ff95e679bcfef69c5127b26b3692d110'

class InstallerTest(unittest.TestCase):
    """Check transactional installation using a disposable prefix and command shims."""
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.prefix = self.root / 'opt'
        self.bin = self.root / 'commands'
        self.bin.mkdir()
        self.archive = self.root / 'client.tar.gz'
        self.client = b'#!/bin/sh\necho "TrustTunnel client v1.1.11"\n'
        self.pack(self.client)
        self.command('uname', '#!/bin/sh\necho aarch64\n')
        self.command('curl', '#!/bin/sh\nwhile [ "$#" -gt 0 ]; do if [ "$1" = -o ]; then cp "$FIXTURE" "$2"; exit; fi; shift; done\nexit 2\n')
        # Verify the actual downloaded bytes while emulating the pinned upstream digest.
        self.command('sha256sum', '#!/usr/bin/env python3\nimport hashlib,os,sys\np=sys.argv[1]\nh=hashlib.sha256(open(p,"rb").read()).hexdigest()\nprint(("'+DIGEST+'" if h==os.environ["FIXTURE_HASH"] else h)+"  "+p)\n')
        self.env = {**os.environ, 'PATH': str(self.bin)+os.pathsep+os.environ['PATH'],
                    'FIXTURE': str(self.archive), 'FIXTURE_HASH': hashlib.sha256(self.archive.read_bytes()).hexdigest()}
        self.target = self.prefix / 'bin/trusttunnel_client'
        self.target.parent.mkdir(parents=True)
        self.target.write_text('old client')

    def command(self, name, content):
        """Replace one external boundary with a deterministic executable fixture."""
        p = self.bin / name
        p.write_text(content)
        p.chmod(0o755)

    def pack(self, content):
        """Build a local release archive with the upstream directory layout."""
        with tarfile.open(self.archive, 'w:gz') as tar:
            info = tarfile.TarInfo('trusttunnel_client-v1.1.11-linux-aarch64/trusttunnel_client')
            info.size = len(content)
            info.mode = 0o755
            tar.addfile(info, io.BytesIO(content))
            license = tarfile.TarInfo('trusttunnel_client-v1.1.11-linux-aarch64/LICENSE')
            license.size = 7
            tar.addfile(license, io.BytesIO(b'license'))

    def run_install(self, *args):
        """Run the real installer without network access or system-prefix writes."""
        return subprocess.run(['sh', str(SCRIPT), '--prefix', str(self.prefix), *args],
                              env=self.env, capture_output=True, text=True)

    def test_verified_client_is_installed_executable(self):
        r = self.run_install('--trusttunnel-only')
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.target.read_bytes(), self.client)
        self.assertTrue(os.access(self.target, os.X_OK))
        self.assertTrue((self.prefix / 'share/keenproxy/trusttunnel/LICENSE').exists())

    def test_corrupt_download_preserves_existing_client(self):
        self.archive.write_bytes(b'corrupt download')
        r = self.run_install('--trusttunnel-only')
        self.assertNotEqual(r.returncode, 0)
        self.assertIn('SHA256 mismatch', r.stderr)
        self.assertEqual(self.target.read_text(), 'old client')

    def test_failed_download_preserves_existing_client(self):
        self.command('curl', '#!/bin/sh\nexit 22\n')
        r = self.run_install('--trusttunnel-only')
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(self.target.read_text(), 'old client')

    def test_incompatible_client_preserves_existing_client(self):
        self.pack(b'#!/bin/sh\necho "missing shared library" >&2\nexit 127\n')
        self.env['FIXTURE_HASH'] = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        r = self.run_install('--trusttunnel-only')
        self.assertNotEqual(r.returncode, 0)
        self.assertIn('cannot run', r.stderr)
        self.assertEqual(self.target.read_text(), 'old client')

    def test_unsupported_cpu_preserves_existing_client(self):
        self.command('uname', '#!/bin/sh\necho mystery-cpu\n')
        r = self.run_install('--trusttunnel-only')
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(self.target.read_text(), 'old client')

    def test_full_install_keeps_config_and_installs_dependency(self):
        config = self.prefix / 'etc/keenproxy/config.yaml'
        config.parent.mkdir(parents=True)
        config.write_text('user configuration')
        app = self.root / 'keenproxy'
        app.write_text('#!/bin/sh\nexit 0\n')
        app.chmod(0o755)
        r = self.run_install('--binary', str(app))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual((self.prefix / 'bin/keenproxy').read_bytes(), app.read_bytes())
        self.assertEqual(config.read_text(), 'user configuration')
        self.assertEqual(self.target.read_bytes(), self.client)

    def test_opt_out_leaves_existing_client(self):
        app = self.root / 'keenproxy'
        app.write_text('#!/bin/sh\nexit 0\n')
        app.chmod(0o755)
        r = self.run_install('--binary', str(app), '--without-trusttunnel')
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.target.read_text(), 'old client')

    def test_dependency_failure_preserves_existing_app(self):
        app = self.prefix / 'bin/keenproxy'
        app.write_text('old app')
        source = self.root / 'keenproxy'
        source.write_text('new app')
        source.chmod(0o755)
        self.archive.write_bytes(b'corrupt')
        r = self.run_install('--binary', str(source))
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(app.read_text(), 'old app')

if __name__ == '__main__':
    unittest.main()

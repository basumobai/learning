"""Isolated installer regressions: no Docker daemon or network required."""
import os
from pathlib import Path
import pty
import select
import shutil
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]


class InstallerPorts(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'scripts').mkdir()
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        for name in ('scripts/install.sh', '.env.example'):
            shutil.copy(ROOT / name, self.root / name)
        self.env = os.environ.copy()
        for key in ('WEB_PORT', 'API_PORT', 'MOCK_BUSY', 'MOCK_OWN'):
            self.env.pop(key, None)
        self.env.update(PATH=f'{self.bin}:{self.env["PATH"]}', MOCK_LOG=str(self.root / 'calls'))
        # ss receives "( sport = :PORT )" as its final argument.
        self.mock('ss', '''port="${*: -1}"; port="${port#*:}"; port="${port% )}"
case " ${MOCK_BUSY:-} " in *" $port "*) echo LISTEN;; esac
exit 0
''')
        self.mock('docker', '''echo "$* | $WEB_PORT/$API_PORT" >> "$MOCK_LOG"
if [[ "$1 $2" == 'volume ls' ]]; then printf '%s' "${MOCK_VOLUMES:-}"; fi
if [[ "$*" == 'compose config --format json' ]]; then echo '{"volumes":{"postgres_data":{"name":"fixture_postgres_data"}}}'; fi
# Match the official image: loopback/socket trust ignores a wrong password;
# only the Compose network connection can detect this fixture's mismatch.
if [[ "$*" == *'sh -c'* && "${MOCK_AUTH_FAIL:-}" == 1 && "$*" == *'psql -h postgres '* ]]; then exit 1; fi
if [[ "$*" == "compose port web 3000" && "${MOCK_OWN:-}" == 1 ]]; then echo "127.0.0.1:$WEB_PORT"; fi
if [[ "$*" == "compose port api 8080" && "${MOCK_OWN:-}" == 1 ]]; then echo "127.0.0.1:$API_PORT"; fi
exit 0
''')
        self.mock('curl', 'echo "$*" >> "$MOCK_LOG"\nexit 0\n')

    def mock(self, name, body):
        p = self.bin / name
        p.write_text('#!/usr/bin/env bash\n' + body)
        p.chmod(0o755)

    def run_install(self, *args, ok=True):
        p = subprocess.run(['bash', str(self.root / 'scripts/install.sh'), *args],
                           env=self.env, stdin=subprocess.DEVNULL, capture_output=True, text=True)
        self.assertEqual(p.returncode == 0, ok, p.stdout + p.stderr)
        return p

    def config(self):
        return (self.root / '.env').read_text()

    def test_custom_noninteractive_and_health_probes(self):
        self.env.update(WEB_PORT='3301', API_PORT='8801')
        p = self.run_install()
        self.assertIn('http://localhost:3301', p.stdout)
        self.assertNotIn('${web_port}', p.stdout)
        self.assertIn('APP_ORIGIN=http://localhost:3301', self.config())
        calls = (self.root / 'calls').read_text()
        self.assertIn('http://127.0.0.1:8801/api/v1/health', calls)
        self.assertIn('http://127.0.0.1:3301/', calls)

    def test_config_only_ignores_occupied_ports(self):
        self.env['MOCK_BUSY'] = '3000 8080'
        self.run_install('--config-only')
        self.assertNotIn('compose up', (self.root / 'calls').read_text())

    def test_existing_volume_without_env_refuses_to_generate_secrets(self):
        self.env['MOCK_VOLUMES'] = 'old_project_postgres_data\n'
        p = self.run_install('--config-only', ok=False)
        self.assertIn('APP_ENCRYPTION_KEY', p.stderr)
        self.assertFalse((self.root / '.env').exists())
        self.assertNotIn('compose up', (self.root / 'calls').read_text())

    def test_authentication_mismatch_preserves_keys_and_does_not_start_api(self):
        self.run_install('--config-only')
        snapshot = self.config()
        self.env.update(MOCK_AUTH_FAIL='1',MOCK_VOLUMES='fixture_postgres_data\n')
        p = self.run_install('--recover',ok=False)
        self.assertEqual(snapshot,self.config())
        self.assertIn('数据库认证检查失败',p.stderr)
        calls = (self.root / 'calls').read_text()
        self.assertNotIn('compose up --build',calls)
        for key in ('POSTGRES_PASSWORD','APP_ENCRYPTION_KEY'):
            value=next(x.split('=',1)[1] for x in snapshot.splitlines() if x.startswith(key+'='))
            self.assertNotIn(value,p.stdout+p.stderr+calls)

    def test_project_rename_refused_without_creating_empty_database(self):
        self.run_install('--config-only')
        self.env['MOCK_VOLUMES']='old_project_postgres_data\n'
        p=self.run_install('--update',ok=False)
        self.assertIn('COMPOSE_PROJECT_NAME',p.stderr)
        self.assertNotIn('compose up',(self.root / 'calls').read_text())

    def test_domain_configuration_and_origin_conflict(self):
        self.run_install('--config-only','--origin','https://www.sumobai.com')
        snapshot=self.config()
        self.assertIn('APP_ORIGIN=https://www.sumobai.com',snapshot)
        self.run_install('--origin','https://wrong.test',ok=False)
        self.assertEqual(snapshot,self.config())

    def test_invalid_origin_is_rejected_before_writing_secrets(self):
        for origin in ('https://example.test/path','http://public.test','https://user:pass@example.test','https://good.test\nPOSTGRES_PASSWORD=bad','https://[bad'):
            with self.subTest(origin=origin):
                self.run_install('--config-only','--origin',origin,ok=False)
                self.assertFalse((self.root/'.env').exists())

    def test_repeat_install_with_data_keeps_secrets(self):
        self.run_install('--config-only')
        snapshot=self.config()
        self.env['MOCK_VOLUMES']='fixture_postgres_data\n'
        self.run_install('--update')
        self.assertEqual(snapshot,self.config())

    def test_update_and_recover_require_existing_configuration(self):
        self.run_install('--update',ok=False)
        self.run_install('--recover',ok=False)
        self.assertFalse((self.root / '.env').exists())

    def test_first_conflict_leaves_no_configuration(self):
        self.env['MOCK_BUSY'] = '8080'
        self.run_install(ok=False)
        self.assertFalse((self.root / '.env').exists())

    def test_invalid_ports_and_duplicates(self):
        for port in ('0', '80', '65536', '03000', 'abc', '9' * 100):
            with self.subTest(port=port):
                self.env['WEB_PORT'] = port
                self.run_install('--config-only', ok=False)
                self.assertFalse((self.root / '.env').exists())
        self.env.update(WEB_PORT='8080', API_PORT='8080')
        self.run_install('--config-only', ok=False)
        self.assertFalse((self.root / '.env').exists())

    def test_restart_allows_only_this_projects_ports(self):
        self.run_install('--config-only')
        snapshot = self.config()
        self.env.update(MOCK_BUSY='3000 8080', MOCK_OWN='1')
        self.run_install()
        self.assertEqual(snapshot, self.config())
        self.env['MOCK_OWN'] = '0'
        self.run_install(ok=False)
        self.assertEqual(snapshot, self.config())

    def test_legacy_env_and_exported_overrides(self):
        self.run_install('--config-only')
        p = self.root / '.env'
        p.write_text('\n'.join(line for line in self.config().splitlines()
                               if not line.startswith(('WEB_PORT=', 'API_PORT='))) + '\n')
        snapshot = self.config()
        self.env.update(WEB_PORT='4999', API_PORT='8999')
        self.run_install()
        self.assertEqual(snapshot, self.config())
        self.assertIn('compose up --build -d | 3000/8080', (self.root / 'calls').read_text())

    def interactive(self, answers):
        master, slave = pty.openpty()
        p = subprocess.Popen(['bash', str(self.root / 'scripts/install.sh')],
                             env=self.env, stdin=slave, stdout=slave, stderr=slave)
        os.close(slave)
        output = b''
        deadline = time.monotonic() + 15
        pending = list(answers)
        try:
            while time.monotonic() < deadline:
                if select.select([master], [], [], .1)[0]:
                    try:
                        data = os.read(master, 65536)
                    except OSError:
                        break
                    output += data
                    if b']: ' in data and pending:
                        os.write(master, pending.pop(0))
                if p.poll() is not None:
                    break
            if p.poll() is None:
                p.kill()
                self.fail('interactive installer timed out: ' + output.decode(errors='replace'))
            return p.wait(), output.decode(errors='replace')
        finally:
            if p.poll() is None:
                p.kill()
            p.wait()
            os.close(master)

    def test_interactive_retries_do_not_pollute_port(self):
        self.env['MOCK_BUSY'] = '3000'
        code, output = self.interactive([b'abc\n', b'\n', b'3301\n', b'3301\n', b'8801\n'])
        self.assertEqual(code, 0, output)
        self.assertIn('WEB_PORT=3301\nAPI_PORT=8801', self.config())
        self.assertIn('APP_ORIGIN=http://localhost:3301', self.config())
        self.assertIn('http://localhost:3301', output)

    def test_cancel_before_configuration(self):
        code, output = self.interactive([b'\x04'])
        self.assertNotEqual(code, 0, output)
        self.assertFalse((self.root / '.env').exists())


if __name__ == '__main__':
    unittest.main()

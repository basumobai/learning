import base64
from pathlib import Path
import tempfile
import unittest
from runner import prepare_input

class RunnerInputTest(unittest.TestCase):
    def test_only_supplied_pages_and_no_shell_command(self):
        with tempfile.TemporaryDirectory() as temp:
            args, prompt, output = prepare_input({'model':'codex-model','prompt':'只识别题目','content':[{'type':'text','text':'Question page 1'},{'type':'image_url','image_url':{'url':'data:image/png;base64,' + base64.b64encode(b'\x89PNG\r\n\x1a\n').decode()}}]}, Path(temp))
            self.assertIn('read-only', args)
            self.assertIn('--ignore-user-config', args)
            self.assertIn('--ephemeral', args)
            self.assertIn('Question page 1', prompt)
            self.assertEqual(output.parent, Path(temp))
            self.assertNotIn('danger-full-access', args)
    def test_remote_image_and_model_injection_are_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            for body in [{'model':'x;cat /secret','prompt':'x','content':'x'},{'prompt':'x','content':[{'type':'image_url','image_url':{'url':'https://private.test/key'}}]}]:
                with self.assertRaises(ValueError):
                    prepare_input(body, Path(temp))

if __name__=='__main__': unittest.main()

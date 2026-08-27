import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("pty_check.py")
SPEC = importlib.util.spec_from_file_location("pty_check", MODULE_PATH)
PTY_CHECK = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(PTY_CHECK)


class PTYCheckTest(unittest.TestCase):
    def test_colour_sgr_count_only_counts_colour(self):
        value = b"plain\x1b[1mbold\x1b[0m\x1b[31mred\x1b[0m\x1b[48;5;24mselected"
        self.assertEqual(PTY_CHECK.colour_sgr_count(value), 2)

    def test_decode_errors_detects_invalid_utf8(self):
        self.assertEqual(PTY_CHECK.decode_errors("界👩‍💻".encode()), 0)
        self.assertEqual(PTY_CHECK.decode_errors(b"bad\xff"), 1)

    def test_capture_is_bounded(self):
        capture = PTY_CHECK.Capture()
        capture.add(b"a" * (PTY_CHECK.MAX_CAPTURE_BYTES + 1))
        self.assertEqual(len(capture.data), PTY_CHECK.MAX_CAPTURE_BYTES)
        self.assertTrue(capture.truncated)

    def test_terminal_mode_comparison_ignores_pending_input_only(self):
        before = [1, 2, 3, 4, 5, 6, [b"a"]]
        after = list(before)
        after[3] = before[3] | getattr(PTY_CHECK.termios, "PENDIN", 0)
        self.assertTrue(PTY_CHECK.terminal_modes_equal(before, after))
        after[0] = 9
        self.assertFalse(PTY_CHECK.terminal_modes_equal(before, after))

    def test_transport_commands_are_explicit(self):
        application = ["/bin/app", "tui"]
        direct = type("Args", (), {"transport": "direct"})
        self.assertEqual(PTY_CHECK.transport_command(direct, application), application)
        tmux = type("Args", (), {"transport": "tmux", "columns": 80, "rows": 24})
        self.assertEqual(PTY_CHECK.transport_command(tmux, application)[0:4], ["tmux", "-L", "kube-memlens-prod009", "new-session"])


if __name__ == "__main__":
    unittest.main()

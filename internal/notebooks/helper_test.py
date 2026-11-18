import pathlib
import queue
import runpy
import unittest

helper = runpy.run_path(str(pathlib.Path(__file__).with_name("helper.py")))
Outputs = helper["Outputs"]
OutputLimit = helper["OutputLimit"]
execute = helper["execute"]


class NotebookOutputsTest(unittest.TestCase):
    def test_nbformat_outputs_and_clear_wait(self):
        out = Outputs(65536, 20)
        out.accept("stream", {"name": "stdout", "text": "a"})
        out.accept("stream", {"name": "stdout", "text": "b"})
        self.assertEqual(out.items, [{"output_type": "stream", "name": "stdout", "text": "ab"}])
        out.accept("clear_output", {"wait": True})
        self.assertEqual(len(out.items), 1)
        out.accept("execute_result", {"execution_count": 3, "data": {"text/plain": "42"}, "metadata": {}})
        self.assertEqual(out.items[0]["output_type"], "execute_result")
        self.assertEqual(out.items[0]["execution_count"], 3)
        out.accept("error", {"ename": "ValueError", "evalue": "bad", "traceback": ["line"]})
        self.assertEqual(out.items[1], {"output_type": "error", "ename": "ValueError", "evalue": "bad", "traceback": ["line"]})
        out.accept("clear_output", {"wait": False})
        self.assertEqual(out.items, [])

    def test_display_updates_all_matching_outputs_without_transient_metadata(self):
        out = Outputs(65536, 20)
        for value in ("one", "two"):
            out.accept("display_data", {"data": {"text/plain": value}, "metadata": {}, "transient": {"display_id": "shared"}})
        out.accept("update_display_data", {"data": {"text/plain": "updated"}, "metadata": {"tag": 1}, "transient": {"display_id": "shared"}})
        self.assertEqual(len(out.items), 2)
        for item in out.items:
            self.assertEqual(item["data"], {"text/plain": "updated"})
            self.assertEqual(item["metadata"], {"tag": 1})
            self.assertNotIn("transient", item)
        self.assertEqual(out.bytes, sum(out.size(item) for item in out.items))

    def test_limits_apply_to_merged_streams_and_updated_displays(self):
        out = Outputs(8192, 20)
        out.accept("stream", {"text": "x" * 2000})
        with self.assertRaises(OutputLimit):
            out.accept("stream", {"text": "x" * 3000})
        out = Outputs(8192, 20)
        out.accept("display_data", {"data": {}, "transient": {"display_id": "large"}})
        with self.assertRaises(OutputLimit):
            out.accept("update_display_data", {"data": {"text/plain": "x" * 9000}, "transient": {"display_id": "large"}})
        out = Outputs(65536, 1)
        out.accept("stream", {"name": "stdout", "text": "a"})
        with self.assertRaises(OutputLimit):
            out.accept("stream", {"name": "stderr", "text": "b"})

    def test_execution_filters_parent_ids_and_waits_for_idle_and_reply(self):
        def msg(kind, content, parent="current"):
            return {"header": {"msg_type": kind}, "parent_header": {"msg_id": parent}, "content": content}

        class Kernel:
            @staticmethod
            def is_alive():
                return True

        class Client:
            def __init__(self):
                self.iopub = [msg("stream", {"text": "ignore"}, "old"), msg("stream", {"text": "kept"}), msg("status", {"execution_state": "idle"})]
                self.shell = [msg("execute_reply", {"status": "error"}, "old"), msg("execute_reply", {"status": "ok", "execution_count": 7})]

            def execute(self, code, **kwargs):
                assert kwargs == {"allow_stdin": False, "stop_on_error": True}
                return "current"

            def get_iopub_msg(self, timeout):
                if self.iopub:
                    return self.iopub.pop(0)
                raise queue.Empty()

            def get_shell_msg(self, timeout):
                if self.shell:
                    return self.shell.pop(0)
                raise queue.Empty()

        result = execute(Kernel(), Client(), "print('kept')", 65536, 20)
        self.assertEqual(result["status"], "ok")
        self.assertEqual(result["execution_count"], 7)
        self.assertEqual(result["outputs"], [{"output_type": "stream", "name": "stdout", "text": "kept"}])


if __name__ == "__main__":
    unittest.main()

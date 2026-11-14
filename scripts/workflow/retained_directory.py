"""Explicit private evidence ownership for long-running workflow environments."""
from pathlib import Path
import os
import tempfile


class RetainedDirectory:
    """TemporaryDirectory-compatible owner whose evidence survives cleanup.

    Process/log cleanup still belongs to the environment. Only explicitly
    requested new private directories use this retained lifecycle.
    """
    def __init__(self,path):
        directory=Path(path)
        directory.mkdir(mode=0o700)
        self.name=str(directory)
        fd=os.open(directory.parent,os.O_RDONLY|os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)

    def cleanup(self):
        # Evidence is deliberately retained, including on failed setup. There
        # is no garbage-collection finalizer that can remove it afterward.
        pass


def run_directory(path=None):
    if path is None:
        return tempfile.TemporaryDirectory(prefix="cui-performance-")
    return RetainedDirectory(path)

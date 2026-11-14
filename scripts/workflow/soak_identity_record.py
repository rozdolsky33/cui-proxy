"""Retain the first pre-run identity independently of the campaign evidence."""

import json
import os
from pathlib import Path


def retain_initial_identity(probe, output, root):
    output, root = Path(output), Path(root)
    if output.resolve().is_relative_to(root.resolve()):
        raise ValueError("soak_expected_identity_not_independent")
    recorded = False

    def capture():
        nonlocal recorded
        identity = probe()
        if not recorded:
            if not isinstance(identity, dict):
                raise ValueError("soak_expected_identity_shape")
            data = (json.dumps(identity, sort_keys=True, allow_nan=False) + "\n").encode()
            fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, "wb") as stream:
                stream.write(data)
                stream.flush()
                os.fsync(stream.fileno())
            recorded = True
        return identity

    return capture

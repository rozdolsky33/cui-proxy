"""Controlled graceful restart of the harness-owned proxy and file vault."""

import time

import harness


class RecoveryFailure(RuntimeError):
    def __init__(self, code, **evidence):
        super().__init__(code)
        self.evidence = evidence


class RestartNotEligible(RuntimeError):
    """No signal was sent; the original scheduled operation may proceed."""


def restart_proxy(env, index, *, continuation, timeout=30, before_signal=None):
    if type(index) is not int or index < 1:
        raise ValueError("soak_restart_index")
    old = env.proxy
    if old.poll() is not None:
        raise RuntimeError("soak_restart_not_running")
    before = env.vault_count()
    args = list(old.args)
    started = time.monotonic()
    checkpoint = before_signal() if before_signal is not None else None
    if before_signal is not None and type(checkpoint) is not dict:
        raise ValueError("restart signal checkpoint invalid")
    signal_started = time.monotonic()
    old.terminate()
    signal_finished = time.monotonic()
    if old.wait(timeout=timeout) != 0:
        raise RuntimeError("soak_restart_exit")
    env.proxy = env.spawn(args, f"proxy-restart-{index}")

    def ready():
        if env.proxy.poll() is not None:
            raise RuntimeError("soak_restart_start")
        try:
            metrics = env.metrics()
        except Exception:
            return False
        return isinstance(metrics, dict) and "runtime" in metrics

    harness.wait_until(ready, timeout, "soak_restart_ready")
    # Vault-size metrics refresh after a request, not at startup. The probe must
    # restore old substitutions without reintroducing originals to seed mappings.
    probe = continuation()
    if probe.get("outcome") != "supported_success" or not all(
            probe.get(field) is True for field in
            ("completed", "semantic_verified", "boundary_verified", "old_substitutions_only")):
        raise RecoveryFailure("soak_restart_continuation")
    after = env.vault_count()
    if type(before) is not int or type(after) is not int or after < before:
        raise RecoveryFailure("soak_restart_vault_loss", before=before if type(before) is int else None,
                              after=after if type(after) is int else None,
                              before_type=type(before).__name__, after_type=type(after).__name__)
    result = {'restart_index': index, 'graceful_exit': True, 'ready': True, 'vault_before': before, 'vault_after': after, 'elapsed_s': time.monotonic() - started, 'continuation_verified': True}
    if before_signal is not None:
        result.update(signal_checkpoint=checkpoint, signal_started_s=signal_started, signal_finished_s=signal_finished)
    return result

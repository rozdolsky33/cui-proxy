"""Mechanical cancellation bounds; provenance and semantic qualification are separate."""

import math
import re


def _number(value):
    if type(value) not in (int, float) or value < 0:
        raise ValueError('invalid_cancellation_time')
    try:
        number = float(value)
    except OverflowError as error:
        raise ValueError('invalid_cancellation_time') from error
    if not math.isfinite(number):
        raise ValueError('invalid_cancellation_time')
    return number


def _window(value):
    if not isinstance(value, dict):
        raise ValueError('missing_cancellation_observation')
    start, end = _number(value.get('started_s')), _number(value.get('ended_s'))
    if end < start:
        raise ValueError('backwards_cancellation_observation')
    return start, end


def _helper(value):
    if not isinstance(value, dict) or set(value) != {
            'pid', 'start', 'parent_pid', 'parent_start', 'generation'}:
        raise ValueError('invalid_cancellation_helper_identity')
    for key in ('pid', 'parent_pid'):
        if type(value[key]) is not int or not 0 < value[key] < 2 ** 31:
            raise ValueError('invalid_cancellation_helper_identity')
    for key in ('start', 'parent_start'):
        if (not isinstance(value[key], str) or not 0 < len(value[key]) <= 128
                or not value[key].strip()
                or any(ord(character) < 32 or ord(character) == 127 for character in value[key])):
            raise ValueError('invalid_cancellation_helper_identity')
    if (value['pid'] == value['parent_pid'] or not isinstance(value['generation'], str)
            or not re.fullmatch(r'[0-9a-f]{32}', value['generation'])):
        raise ValueError('invalid_cancellation_helper_identity')
    return tuple(value[key] for key in ('pid', 'start', 'parent_pid', 'parent_start', 'generation'))


def _observation(value, queued, markers):
    window = _window(value)
    if (type(value.get('active')) is not int or value['active'] != 2
            or type(value.get('queued')) is not int or value['queued'] != queued
            or type(value.get('marker_count')) is not int or value['marker_count'] != markers):
        raise ValueError('cancellation_state_not_established')
    rows = value.get('helpers')
    if not isinstance(rows, list) or len(rows) != 2:
        raise ValueError('cancellation_helper_coverage')
    helpers = {_helper(row) for row in rows}
    if (len(helpers) != 2 or len({row[0] for row in helpers}) != 2
            or len({row[2:] for row in helpers}) != 1):
        raise ValueError('ambiguous_cancellation_helpers')
    return window, helpers


def _step(value, limit, final_markers):
    if not isinstance(value, dict):
        raise ValueError('missing_cancellation_step')
    before, helpers_before = _observation(value.get('before'), 1, 2)
    after, helpers_after = _observation(value.get('after'), 0, final_markers)
    start = _number(value.get('request_started_s'))
    cancelled = _number(value.get('cancel_s'))
    transport_end = _number(value.get('transport_end_s'))
    if (value.get('closed_by_probe') is not True
            or value.get('headers_received_before_cancel') is not False
            or start > before[0] or before[1] > cancelled
            or after[0] < cancelled or transport_end < cancelled):
        raise ValueError('unproven_cancellation_sequence')
    if transport_end - start > 45:
        raise ValueError('cancellation_request_deadline')
    completed = max(after[1], transport_end)
    if completed - cancelled > limit:
        raise ValueError('cancellation_release_limit')
    return dict(before=before, after=after, helpers_before=helpers_before,
                helpers_after=helpers_after, cancel=cancelled, completed=completed,
                elapsed_ms=(completed - cancelled) * 1000)


def evaluate_trial(trial):
    """Check observed bounds without claiming complete cancellation qualification.

    The caller must separately bind these observations to owned processes,
    immutable artifacts, source identity, warmup, receipts, audit and cleanup.
    Observation end times provide conservative upper bounds; a marker's own
    timestamp is never used as a substitute for the observer's clock.
    """
    if not isinstance(trial, dict) or trial.get('gate_remained_armed') is not True:
        raise ValueError('cancellation_gate_not_held')
    queued = _step(trial.get('queued'), 1, 2)
    active = _step(trial.get('active'), 2, 3)
    if queued['helpers_before'] != queued['helpers_after']:
        raise ValueError('queued_cancellation_started_helper')
    if (active['helpers_before'] != queued['helpers_after']
            or active['before'][0] < queued['completed']):
        raise ValueError('cancellation_trials_not_contiguous')
    details = trial['active']
    cancelled_helper = _helper(details.get('cancelled_helper'))
    survivor = _helper(details.get('survivor_helper'))
    if cancelled_helper == survivor or {cancelled_helper, survivor} != active['helpers_before']:
        raise ValueError('cancellation_target_not_bound')
    stopped, admitted = details.get('stopped'), details.get('admitted')
    stopped_window, admitted_window = _window(stopped), _window(admitted)
    if (stopped.get('state') not in ('absent', 'zombie')
            or _helper(stopped.get('helper')) != cancelled_helper):
        raise ValueError('cancelled_helper_not_terminal')
    replacement = _helper(admitted.get('helper'))
    if (replacement[0] in {cancelled_helper[0], survivor[0]}
            or replacement[2:] != survivor[2:]
            or {survivor, replacement} != active['helpers_after']):
        raise ValueError('replacement_admission_not_bound')
    for start, end in (stopped_window, admitted_window):
        if start < active['cancel'] or end > active['after'][1]:
            raise ValueError('cancellation_release_observation_unbounded')
    return dict(bounds_passed=True, cancellation_qualified=False,
                performance_qualified=False, queued_ms=queued['elapsed_ms'],
                active_ms=active['elapsed_ms'])

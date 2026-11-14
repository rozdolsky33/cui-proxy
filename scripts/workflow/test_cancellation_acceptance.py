import copy
import unittest

from cancellation_acceptance import evaluate_trial


def helper(pid):
    return dict(pid=pid, start='start-' + str(pid), parent_pid=90,
                parent_start='proxy-start', generation='a' * 32)


def observation(start, end, active, queued, helpers, markers):
    return dict(started_s=start, ended_s=end, active=active, queued=queued,
                helpers=helpers, marker_count=markers)


def trial():
    first, survivor, replacement = helper(101), helper(102), helper(103)
    return dict(gate_remained_armed=True,
        queued=dict(request_started_s=10.2, cancel_s=11, transport_end_s=11.2,
            closed_by_probe=True, headers_received_before_cancel=False,
            before=observation(10.8, 10.9, 2, 1, [first, survivor], 2),
            after=observation(11.1, 11.3, 2, 0, [first, survivor], 2)),
        active=dict(request_started_s=9, cancel_s=13, transport_end_s=13.2,
            closed_by_probe=True, headers_received_before_cancel=False,
            before=observation(12.8, 12.9, 2, 1, [first, survivor], 2),
            after=observation(13.3, 13.6, 2, 0, [survivor, replacement], 3),
            cancelled_helper=first, survivor_helper=survivor,
            stopped=dict(helper=first, state='absent', started_s=13.2, ended_s=13.4),
            admitted=dict(helper=replacement, started_s=13.3, ended_s=13.5)))


class CancellationBoundsTests(unittest.TestCase):
    def test_proves_bounds_but_never_claims_full_qualification(self):
        result = evaluate_trial(trial())
        self.assertTrue(result['bounds_passed'])
        self.assertFalse(result['cancellation_qualified'])
        self.assertFalse(result['performance_qualified'])
        self.assertAlmostEqual(result['queued_ms'], 300)
        self.assertAlmostEqual(result['active_ms'], 600)

    def test_exact_limits_pass_and_larger_observation_bounds_fail(self):
        report = trial()
        report['queued']['after']['ended_s'] = 12
        report['active']['after']['ended_s'] = 15
        self.assertTrue(evaluate_trial(report)['bounds_passed'])
        for kind, end in [('queued', 12.00001), ('active', 15.00001)]:
            bad = copy.deepcopy(report)
            bad[kind]['after']['ended_s'] = end
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                evaluate_trial(bad)

    def test_rejects_nonfinite_boolean_and_backwards_times(self):
        for value in (float('nan'), float('inf'), True, -1):
            bad = trial()
            bad['queued']['cancel_s'] = value
            with self.subTest(value=value), self.assertRaises(ValueError):
                evaluate_trial(bad)
        bad = trial()
        bad['active']['admitted']['started_s'] = 12
        with self.assertRaises(ValueError):
            evaluate_trial(bad)

    def test_requires_observed_queue_and_surviving_helpers(self):
        for kind, field, value in [('queued', 'queued', 0), ('active', 'active', 1)]:
            bad = trial()
            bad[kind]['before'][field] = value
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                evaluate_trial(bad)
        bad = trial()
        bad['queued']['after']['helpers'][0] = helper(104)
        with self.assertRaises(ValueError):
            evaluate_trial(bad)
        bad = trial()
        bad['active']['after']['helpers'] = [helper(101), helper(103)]
        with self.assertRaises(ValueError):
            evaluate_trial(bad)

    def test_rejects_pid_reuse_wrong_parent_and_nonterminal_stop(self):
        for mutation in ('reuse', 'parent', 'running'):
            bad = trial()
            if mutation == 'reuse':
                replacement = helper(101)
                replacement['start'] = 'different-start'
                bad['active']['admitted']['helper'] = replacement
                bad['active']['after']['helpers'][1] = replacement
            elif mutation == 'parent':
                bad['active']['admitted']['helper']['parent_pid'] = 91
            else:
                bad['active']['stopped']['state'] = 'running'
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                evaluate_trial(bad)

    def test_rejects_premature_headers_lost_gate_and_unbounded_requests(self):
        for mutation in ('headers', 'gate', 'deadline', 'marker'):
            bad = trial()
            if mutation == 'headers':
                bad['queued']['headers_received_before_cancel'] = True
            elif mutation == 'gate':
                bad['gate_remained_armed'] = False
            elif mutation == 'deadline':
                bad['active']['request_started_s'] = -40
            else:
                bad['queued']['after']['marker_count'] = 3
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                evaluate_trial(bad)

    def test_rejects_oversized_time_and_blank_process_identity(self):
        bad = trial()
        bad['queued']['cancel_s'] = 2 ** 1024
        with self.assertRaises(ValueError):
            evaluate_trial(bad)
        bad = trial()
        bad['queued']['before']['helpers'][0]['start'] = ' '
        with self.assertRaises(ValueError):
            evaluate_trial(bad)

    def test_enforces_45_second_transport_deadline(self):
        report = trial()
        def shift(value):
            if isinstance(value, dict):
                for key, item in value.items():
                    if key.endswith('_s') and type(item) in (int, float):
                        value[key] = item + 50
                    elif isinstance(item, (dict, list)):
                        shift(item)
            elif isinstance(value, list):
                for item in value:
                    shift(item)
        shift(report)
        report['active']['request_started_s'] = 1
        with self.assertRaisesRegex(ValueError, 'cancellation_request_deadline'):
            evaluate_trial(report)


if __name__ == '__main__':
    unittest.main()

import unittest

import harness
import observation
import test_observation


class AdmissionObservationTests(unittest.TestCase):
    def test_admission_wait_is_observable(self):
        text, image = test_observation.ObservationTests().snapshots()
        for snapshot in (text, image):
            snapshot["stages_ms"]["admission_wait"] = dict(snapshot["stages_ms"]["ingress_read"])
        observation.check_snapshots(text, image)

    def test_unknown_stage_remains_rejected(self):
        text, image = test_observation.ObservationTests().snapshots()
        text["stages_ms"]["unexpected_stage"] = dict(text["stages_ms"]["ingress_read"])
        with self.assertRaises(harness.CheckFailure):
            observation.check_snapshots(text, image)

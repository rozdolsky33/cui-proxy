"""Bind sealed phase outcomes to deterministic image replay selections."""
import copy
from soak_schedule import plan_request


def _identity(row):
    if not isinstance(row,dict):
        raise ValueError("fixture_replay_row")
    identity=row.get("identity")
    if not isinstance(identity,(list,tuple)) or len(identity)!=3:
        raise ValueError("fixture_replay_identity")
    phase,client,serial=identity
    plan_request(client,serial,phase)  # Reject booleans and invalid coordinates.
    return tuple(identity)


def bind_fixture_replay(execute,report,corpus):
    """Resolve all image selections before admitting any replay operation.

    The recorded-phase runner remains responsible for timing/drain validation;
    this binding validates the exact outcome/schedule identity bijection and
    fixture correspondence without changing the journal schema.
    """
    if not isinstance(report,dict) or report.get("completed") is not True:
        raise ValueError("fixture_replay_incomplete")
    if not isinstance(report.get("schedule"),list) or not isinstance(report.get("results"),list):
        raise ValueError("fixture_replay_schema")
    planned={}
    for row in report["schedule"]:
        identity=_identity(row)
        if identity in planned or identity[0]!=report.get("phase"):
            raise ValueError("fixture_replay_schedule")
        canonical=plan_request(identity[1],identity[2],identity[0])
        if any(row.get(k)!=v for k,v in canonical.items() if k!="identity"):
            raise ValueError("fixture_replay_schedule")
        planned[identity]=copy.deepcopy(row)
    observed={}
    for row in report["results"]:
        identity=_identity(row)
        if identity in observed or not isinstance(row.get("request"),dict):
            raise ValueError("fixture_replay_results")
        observed[identity]=row["request"]
    if not planned or set(planned)!=set(observed):
        raise ValueError("fixture_replay_bijection")
    evidence={}
    for identity,plan in planned.items():
        if plan["category"]=="image":
            proof=copy.deepcopy(observed[identity].get("fixture_evidence"))
            corpus.resolve(plan,proof)
            evidence[identity]=proof

    def bound(plan):
        identity=_identity(plan)
        expected=planned.get(identity)
        if expected is None:
            raise ValueError("fixture_replay_unknown")
        keys=("category","scenario","size_bytes","churn_indices")
        if any(plan.get(k)!=expected.get(k) for k in keys):
            raise ValueError("fixture_replay_plan_changed")
        forwarded=copy.deepcopy(plan)
        if identity in evidence:
            if "fixture_evidence" in plan and plan["fixture_evidence"]!=evidence[identity]:
                raise ValueError("fixture_replay_selection_changed")
            forwarded["fixture_evidence"]=copy.deepcopy(evidence[identity])
        return execute(forwarded)
    return bound

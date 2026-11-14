"""Value-free, bounded exception metadata for captured workflow failures."""

SAFE_FAILURE_TYPES = frozenset(('Exception', 'RuntimeError', 'TypeError', 'ValueError',
    'AttributeError', 'KeyError', 'OSError', 'FileNotFoundError', 'PermissionError',
    'TimeoutError', 'AssertionError', 'KeyboardInterrupt', 'SystemExit'))


def failure_metadata(error):
    from pathlib import Path
    types, lines, seen = [], [], set()
    directory = Path(__file__).resolve().parent
    while error is not None and id(error) not in seen and len(types) < 3:
        seen.add(id(error))
        name = type(error).__name__
        types.append(name if type(error).__module__ == 'builtins' and name in SAFE_FAILURE_TYPES else 'Exception')
        frame = error.__traceback__
        while frame is not None and len(lines) < 12:
            if Path(frame.tb_frame.f_code.co_filename).resolve().parent == directory:
                lines.append(frame.tb_lineno)
            frame = frame.tb_next
        error = error.__cause__ or error.__context__
    return dict(exception_types=types, workflow_lines=lines)


def valid_failure_metadata(value):
    if not isinstance(value, dict) or set(value) != {'exception_types', 'workflow_lines'}:
        return False
    types, lines = value['exception_types'], value['workflow_lines']
    return (isinstance(types, list) and 0 < len(types) <= 3
            and all(isinstance(x, str) and x in SAFE_FAILURE_TYPES for x in types)
            and isinstance(lines, list) and len(lines) <= 12
            and all(type(x) is int and 0 < x < 100000 for x in lines))

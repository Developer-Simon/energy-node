import json


def load_state(config, state):
    """Read state from config.state_file; on any exception return silently.

    For new format (units dict present), calls state.load_dict(data).
    For legacy format, calls state.load_legacy_dict(data, config.topology).
    """
    try:
        data = json.loads(config.state_file.read_text())
    except Exception:
        return  # No/broken state file -> start with defaults

    stored = data.get("units")
    if isinstance(stored, dict):
        state.load_dict(data)
    else:
        state.load_legacy_dict(data, config.topology)


def save_state(config, state):
    """Write state to config.state_file; swallow all exceptions."""
    try:
        config.state_file.parent.mkdir(parents=True, exist_ok=True)
        config.state_file.write_text(json.dumps(state.to_dict()))
    except Exception:
        pass  # Persistence is a nice-to-have, not a show-stopper

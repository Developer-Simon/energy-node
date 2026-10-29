from fakelog import Log


def test_add_keeps_level_and_defaults_to_info():
    log = Log()
    log.add("mqtt", "eins")
    log.add("mqtt", "zwei", "warn")
    log.add("mqtt", "drei", "quatsch")
    _, entries = log.since(0)
    assert [e["level"] for e in entries] == ["info", "warn", "info"]
    assert entries[1]["source"] == "mqtt" and entries[1]["text"] == "zwei"


def test_ring_drops_oldest():
    log = Log(size=2)
    for text in ("a", "b", "c"):
        log.add("x", text)
    last, entries = log.since(0)
    assert last == 3 and [e["text"] for e in entries] == ["b", "c"]

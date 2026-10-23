"""Tests fuer die Regler-Rechnung und die gemeinsame Wertquantisierung."""

import regulator


def reg(**overrides):
    base = {
        "output": {"min": 50, "max": 800, "step": 10, "decimals": 0},
        "direction": "producer",
        "target_w": 30,
        "deadband_w": 20,
        "gain": 0.8,
    }
    base.update(overrides)
    return base


def test_producer_lowers_the_limit_on_feed_in():
    assert regulator.compute_step(reg(), net=-470, base=800, last_value=800).payload == "400"


def test_consumer_raises_the_setpoint_on_feed_in():
    r = reg(direction="consumer", target_w=0, output={"min": 0, "max": 3700, "step": 100, "decimals": 0})
    assert regulator.compute_step(r, net=-1500, base=0, last_value=None).payload == "1200"


def test_inside_the_deadband_nothing_is_sent():
    step = regulator.compute_step(reg(), net=45, base=500, last_value=500)
    assert (step.value, step.reason) == (None, "deadband")


def test_value_is_floored_to_step_not_rounded():
    assert regulator.compute_step(reg(), net=-100, base=800, last_value=None).payload == "690"  # 800-104 = 696 -> 690


def test_value_is_clamped_to_min_and_max():
    assert regulator.compute_step(reg(), net=-5000, base=800, last_value=None).payload == "50"
    assert regulator.compute_step(reg(), net=5000, base=800, last_value=None).payload == "800"


def test_max_change_limits_the_step_against_the_last_value():
    assert regulator.compute_step(reg(max_change_w=100), net=-470, base=800, last_value=800).payload == "700"


def test_unchanged_value_is_not_sent_again():
    step = regulator.compute_step(reg(), net=60, base=780, last_value=800)  # 780+24=804 -> 800
    assert (step.value, step.reason) == (None, "unchanged")


def test_quantize_matches_the_old_publish_formatting():
    assert regulator.quantize(12.345, decimals=1) == (12.3, "12.3")
    assert regulator.quantize(1234.0) == (1234.0, "1234.0")

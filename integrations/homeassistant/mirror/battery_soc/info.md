<img src="https://raw.githubusercontent.com/Developer-Simon/ha-battery-soc/main/custom_components/battery_soc/brand/icon.png" alt="Battery SoC icon" width="72" align="right">

Coulomb-counting state-of-charge estimator for LiFePO4 battery banks (single,
parallel, or series). It integrates net charge/discharge power over time and
recalibrates against cell voltage only near the flat curve's ends, where
LiFePO4 voltage actually moves. Point it at your existing power and voltage
sensors — no extra hardware. Monitoring and diagnostics only; not a BMS.

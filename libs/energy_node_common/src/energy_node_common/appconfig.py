"""Zentrale Anwendungskonfiguration aus /etc/energy-node/config.json.

Ersetzt die frueheren *.env-Dateien aller Python-Dienste. Die Datei ist
Pflicht: fehlt sie oder ist sie ungueltig, startet der Dienst nicht
(fail-closed, wie zuvor das EnvironmentFile ohne fuehrendes '-').

Die Validierung ist bewusst handgeschrieben - pyproject.toml fuehrt nur
paho-mqtt, und eine Schema-Bibliothek waere die einzige Abhaengigkeit, die
allein dafuer hinzukaeme. Die Struktur wird zusaetzlich vom Dashboard gegen
dashboard/internal/appconfig/config.schema.json geprueft; beide Fassungen
muessen zusammen geaendert werden.
"""

from __future__ import annotations

import json
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Optional, Sequence, Tuple

DEFAULT_CONFIG_PATH = "/etc/energy-node/config.json"
SCHEMA_VERSION = 1

_NUMERIC_SERVICE_FIELDS = ("poll_interval_s", "diagnostic_poll_multiplier", "http_timeout_s")


class ConfigError(Exception):
    """Konfiguration fehlt, ist ungueltig oder passt nicht zur Version."""


def _found(value) -> str:
    """Den vorgefundenen Wert so darstellen, wie er in der Datei steht."""
    return json.dumps(value, ensure_ascii=False)


def _section(data: dict, key: str, path: str) -> dict:
    if key not in data:
        raise ConfigError(f"{path}: {key}: Abschnitt fehlt")
    value = data[key]
    if not isinstance(value, dict):
        raise ConfigError(f"{path}: {key}: erwartet Objekt, gefunden {_found(value)}")
    return value


def _text(section: dict, key: str, path: str, prefix: str, *, allow_empty: bool = False) -> str:
    if key not in section:
        raise ConfigError(f"{path}: {prefix}.{key}: Feld fehlt")
    value = section[key]
    if not isinstance(value, str):
        raise ConfigError(f"{path}: {prefix}.{key}: erwartet Zeichenkette, gefunden {_found(value)}")
    if not value and not allow_empty:
        raise ConfigError(f"{path}: {prefix}.{key}: darf nicht leer sein")
    return value


def _number(section: dict, key: str, path: str, prefix: str) -> float:
    if key not in section:
        raise ConfigError(f"{path}: {prefix}.{key}: Feld fehlt")
    value = section[key]
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ConfigError(f"{path}: {prefix}.{key}: erwartet Zahl, gefunden {_found(value)}")
    if value <= 0:
        raise ConfigError(f"{path}: {prefix}.{key}: erwartet Wert groesser 0, gefunden {_found(value)}")
    return float(value)


@dataclass(frozen=True)
class MQTTConfig:
    host: str
    port: int
    username: str
    password_file: str

    def password(self) -> str:
        """Das Passwort aus password_file lesen.

        Leerer Pfad bedeutet "kein Passwort". Ein gesetzter Pfad, dessen
        Datei fehlt, ist ein Startfehler - kein stilles leeres Passwort.
        Nachlaufender Weissraum wird abgeschnitten, damit ein per Editor
        angehaengter Zeilenumbruch nicht Teil des Passworts wird.
        """
        if not self.password_file:
            return ""
        try:
            raw = Path(self.password_file).read_text(encoding="utf-8")
        except OSError as exc:
            raise ConfigError(f"{self.password_file}: Passwortdatei nicht lesbar: {exc}") from exc
        return raw.rstrip("\r\n\t ")


@dataclass(frozen=True)
class ServiceConfig:
    name: str
    service_id: str
    poll_interval_s: Optional[float] = None
    diagnostic_poll_multiplier: Optional[float] = None
    http_timeout_s: Optional[float] = None


@dataclass(frozen=True)
class NodeConfig:
    device_id: str
    device_name: str
    managed_bridges: Tuple[str, ...]
    poll_interval_s: float
    diagnostic_poll_multiplier: float


@dataclass(frozen=True)
class DashboardConfig:
    node_device_id: str
    node_device_name: str
    node_poll_interval_s: float
    node_diagnostic_poll_multiplier: float


@dataclass(frozen=True)
class PathsConfig:
    devices_dir: str
    data_dir: str


@dataclass(frozen=True)
class AppConfig:
    path: str
    schema_version: int
    mqtt: MQTTConfig
    paths: PathsConfig
    log_level: str
    node: NodeConfig
    dashboard: DashboardConfig
    services: Dict[str, ServiceConfig]

    def service(self, name: str) -> ServiceConfig:
        try:
            return self.services[name]
        except KeyError:
            raise ConfigError(f"{self.path}: services.{name}: Abschnitt fehlt") from None

    def devices_config(self, name: str) -> Path:
        """devices_dir/<name>_devices.json - die Konvention an einer Stelle."""
        return Path(self.paths.devices_dir) / f"{name}_devices.json"

    def devices_schema(self, name: str) -> Path:
        return Path(self.paths.devices_dir) / f"{name}_devices.schema.json"

    def rules_config(self) -> Path:
        return Path(self.paths.devices_dir) / "automation_rules.json"

    def history_file(self) -> Path:
        return Path(self.paths.devices_dir) / "automation_history.json"


def config_path_from_argv(argv: Optional[Sequence[str]] = None) -> str:
    """--config <pfad> bzw. --config=<pfad> auswerten, sonst der feste Pfad."""
    args = list(sys.argv[1:] if argv is None else argv)
    for index, arg in enumerate(args):
        if arg == "--config":
            if index + 1 >= len(args):
                raise ConfigError("--config erwartet einen Pfad")
            return args[index + 1]
        if arg.startswith("--config="):
            value = arg.split("=", 1)[1]
            if not value:
                raise ConfigError("--config erwartet einen Pfad")
            return value
    return DEFAULT_CONFIG_PATH


def _load_manifests(config_dir: Path, path: str) -> Dict[str, Tuple[str, ...]]:
    """Die aktive Dienstmenge samt Pflichtfeldern aus <config-dir>/manifests/.

    Genau eine Wahrheit ueber den Funktionsumfang eines Node: je Dienst ein
    manifest.json mit "service_id" und "required". Fehlt das Verzeichnis oder
    ist es leer, startet der Dienst nicht (fail-closed).
    """
    manifests_dir = config_dir / "manifests"
    try:
        files = sorted(manifests_dir.glob("*.json"))
    except OSError as exc:
        raise ConfigError(f"{manifests_dir}: Manifest-Verzeichnis nicht lesbar: {exc}") from exc
    if not files:
        raise ConfigError(
            f"{manifests_dir}: kein Dienst-Manifest gefunden (erwartet <service_id>.json je Dienst)"
        )
    required_by_service: Dict[str, Tuple[str, ...]] = {}
    for file in files:
        try:
            entry = json.loads(file.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            raise ConfigError(f"{file}: Manifest nicht lesbar: {exc}") from exc
        service_id = entry.get("service_id")
        if not isinstance(service_id, str) or not service_id:
            raise ConfigError(f"{file}: service_id fehlt oder ist leer")
        raw_required = entry.get("required", [])
        if not isinstance(raw_required, list) or not all(
            isinstance(item, str) and item for item in raw_required
        ):
            raise ConfigError(f"{file}: required erwartet eine Liste nicht-leerer Zeichenketten")
        if service_id in required_by_service:
            raise ConfigError(f"{file}: service_id {service_id!r} doppelt vergeben")
        required_by_service[service_id] = tuple(raw_required)
    return required_by_service


def _load_services(
    data: dict, path: str, required_by_service: Dict[str, Tuple[str, ...]]
) -> Dict[str, ServiceConfig]:
    section = _section(data, "services", path)
    services: Dict[str, ServiceConfig] = {}
    for name, required in required_by_service.items():
        if name not in section:
            raise ConfigError(f"{path}: services.{name}: Abschnitt fehlt (Manifest vorhanden)")
        entry = section[name]
        prefix = f"services.{name}"
        if not isinstance(entry, dict):
            raise ConfigError(f"{path}: {prefix}: erwartet Objekt, gefunden {_found(entry)}")
        values = {}
        for field_name in _NUMERIC_SERVICE_FIELDS:
            if field_name in required or field_name in entry:
                values[field_name] = _number(entry, field_name, path, prefix)
            else:
                values[field_name] = None
        services[name] = ServiceConfig(
            name=name,
            service_id=_text(entry, "service_id", path, prefix),
            **values,
        )
    for name in section:
        if name not in required_by_service:
            raise ConfigError(
                f"{path}: services.{name}: kein Manifest unter manifests/ (unbekannter Dienst)"
            )
    return services


def _load_node(data: dict, path: str, services: Dict[str, ServiceConfig]) -> NodeConfig:
    section = _section(data, "node", path)
    raw_bridges = section.get("managed_bridges")
    if not isinstance(raw_bridges, list) or not raw_bridges:
        raise ConfigError(f"{path}: node.managed_bridges: erwartet nicht-leere Liste, gefunden {_found(raw_bridges)}")
    bridges = []
    for index, entry in enumerate(raw_bridges):
        if not isinstance(entry, str) or not entry:
            raise ConfigError(
                f"{path}: node.managed_bridges[{index}]: erwartet nicht-leere Zeichenkette, gefunden {_found(entry)}"
            )
        if entry not in services:
            raise ConfigError(f"{path}: node.managed_bridges[{index}]: services.{entry} fehlt")
        bridges.append(entry)
    return NodeConfig(
        device_id=_text(section, "device_id", path, "node"),
        device_name=_text(section, "device_name", path, "node"),
        managed_bridges=tuple(bridges),
        poll_interval_s=_number(section, "poll_interval_s", path, "node"),
        diagnostic_poll_multiplier=_number(section, "diagnostic_poll_multiplier", path, "node"),
    )


def _load_dashboard(data: dict, path: str) -> DashboardConfig:
    section = _section(data, "dashboard", path)
    return DashboardConfig(
        node_device_id=_text(section, "node_device_id", path, "dashboard"),
        node_device_name=_text(section, "node_device_name", path, "dashboard"),
        node_poll_interval_s=_number(section, "node_poll_interval_s", path, "dashboard"),
        node_diagnostic_poll_multiplier=_number(section, "node_diagnostic_poll_multiplier", path, "dashboard"),
    )


def load(path: Optional[str] = None) -> AppConfig:
    """Die zentrale Konfiguration lesen oder mit ConfigError abbrechen."""
    path = path or DEFAULT_CONFIG_PATH
    try:
        raw_text = Path(path).read_text(encoding="utf-8")
    except FileNotFoundError:
        raise ConfigError(
            f"{path}: Konfigurationsdatei fehlt (anderer Pfad ueber --config <pfad>)"
        ) from None
    except OSError as exc:
        raise ConfigError(f"{path}: nicht lesbar: {exc}") from exc

    try:
        data = json.loads(raw_text)
    except json.JSONDecodeError as exc:
        raise ConfigError(
            f"{path}: ungueltiges JSON in Zeile {exc.lineno}, Spalte {exc.colno}: {exc.msg}"
        ) from exc

    if not isinstance(data, dict):
        raise ConfigError(f"{path}: erwartet Objekt, gefunden {_found(data)}")

    version = data.get("schema_version")
    if isinstance(version, bool) or not isinstance(version, int):
        raise ConfigError(f"{path}: schema_version: erwartet Zahl, gefunden {_found(version)}")
    if version != SCHEMA_VERSION:
        raise ConfigError(
            f"{path}: schema_version {version} passt nicht zu erwarteter Version {SCHEMA_VERSION}"
        )

    mqtt_section = _section(data, "mqtt", path)
    paths_section = _section(data, "paths", path)
    logging_section = _section(data, "logging", path)
    manifests = _load_manifests(Path(path).parent, path)
    services = _load_services(data, path, manifests)

    return AppConfig(
        path=path,
        schema_version=version,
        mqtt=MQTTConfig(
            host=_text(mqtt_section, "host", path, "mqtt"),
            port=int(_number(mqtt_section, "port", path, "mqtt")),
            username=_text(mqtt_section, "username", path, "mqtt", allow_empty=True),
            password_file=_text(mqtt_section, "password_file", path, "mqtt", allow_empty=True),
        ),
        paths=PathsConfig(
            devices_dir=_text(paths_section, "devices_dir", path, "paths"),
            data_dir=_text(paths_section, "data_dir", path, "paths"),
        ),
        log_level=_text(logging_section, "level", path, "logging"),
        node=_load_node(data, path, services),
        dashboard=_load_dashboard(data, path),
        services=services,
    )

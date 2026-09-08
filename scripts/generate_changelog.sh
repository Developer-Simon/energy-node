#!/usr/bin/env bash
#
# Erzeugt <komponente>/CHANGELOG.md aus den Conventional-Commit-Prefixes
# (feat, fix, refactor, perf, docs, test, style, chore, dev, build, ci) der
# Commits, die die jeweilige Komponente betreffen.
#
# Jede Komponente hat ihre eigene VERSION-Datei, deren Patch-Level bei jedem
# Commit auf main automatisch hochgezaehlt wird (siehe git-hooks/pre-commit)
# - Major/Minor werden von Hand gepflegt. Ein neuer Abschnitt im Changelog
# entsteht deshalb nur, wenn sich Major oder Minor aendert, oder wenn der
# Commit selbst getaggt ist; reine Patch-Bumps bleiben im selben Abschnitt.
#
# Usage: ./generate_changelog.sh [--freeze-before <version>] [--rebuild] [<target>|all]
#   <target>: dashboard | services | common | battery_soc_core | ha-integration
#
# Ohne Zielangabe (oder mit "all") werden alle Targets nacheinander generiert.
#
# ha-integration (integrations/homeassistant/, die HACS-Integration) liest
# ihre Version aus custom_components/battery_soc/manifest.json ("version",
# nackte Semver ohne "v" - HACS/hassfest-Pflicht) statt aus einer
# VERSION-Datei; die Changelog-Ueberschriften nutzen trotzdem "vX.Y.Z" wie
# ueberall sonst, siehe scripts/publish_mirror.sh.
#
# Auto-Freeze (Standardverhalten):
#   Die bestehende CHANGELOG.md wird in Abschnitte zerlegt. Vom ersten
#   Abschnitt an (von oben), den die aktuelle Commit-Historie nicht mehr
#   hergibt - also mind. eine "(hash)"-Referenz, die "git log" fuer diese
#   Komponente nicht mehr kennt, oder ein Abschnitt ganz ohne Hashes -, wird
#   der Rest der Datei unveraendert uebernommen ("eingefroren"). Alles
#   darueber (was Git noch reproduzieren kann) wird wie gehabt neu aus der
#   Commit-Historie gebaut und oben angefuegt. So kann kein Lauf von Hand
#   gepflegte bzw. aus dem Vorgaenger-Repo uebernommene Alt-Historie mehr
#   plattmachen (History-freier Fork), und wiederholte Laeufe sind idempotent.
#   Ohne solchen Abschnitt (frische Datei, oder Git kennt noch alles) ist
#   Auto-Freeze wirkungslos und es wird komplett neu gebaut.
#
# --rebuild:
#   Auto-Freeze aus. Die CHANGELOG.md wird komplett aus der Commit-Historie
#   neu aufgebaut (nur noch --freeze-before / die Komponenten-Untergrenze
#   min_freeze frieren dann Abschnitte ein). Bewusster Voll-Neuaufbau.
#
# --freeze-before <version>:
#   Abschnitte mit Major.Minor < <version> werden unveraendert aus der
#   bestehenden CHANGELOG.md uebernommen (z.B. von Hand nachbearbeitete
#   Eintraege bleiben so erhalten) statt aus der Commit-Historie neu gebaut
#   zu werden. Ab <version> (inklusive) wird wie gewohnt aus der
#   Commit-Historie neu generiert, damit der aktuell offene Abschnitt neue
#   Commits aufnimmt. <version> akzeptiert "vX.Y", "X.Y" oder "vX.Y.Z".
#   Wirkt zusaetzlich zu Auto-Freeze: friert hoechstens mehr Abschnitte ein
#   (auch solche, die Git noch reproduzieren koennte), nie weniger.

set -euo pipefail

ALL_TARGETS=(dashboard services common battery_soc_core ha-integration)

usage() {
  echo "Usage: $(basename "$0") [--freeze-before <version>] [--rebuild] [$(IFS='|'; echo "${ALL_TARGETS[*]}")|all]" >&2
}

declare -A LABELS=(
  [feat]="Features" [fix]="Fixes" [refactor]="Refactors" [perf]="Performance"
  [docs]="Documentation" [test]="Tests" [style]="Style" [chore]="Chores"
  [dev]="Dev" [build]="Build" [ci]="CI" [other]="Other"
)
TYPE_ORDER=(feat fix refactor perf docs test style chore dev build ci other)
VERSION_RE='^v([0-9]+)\.([0-9]+)\.([0-9]+)$'
FREEZE_VERSION_RE='^v?([0-9]+)\.([0-9]+)(\.[0-9]+)?$'
COMMIT_RE_SCOPED='^([a-zA-Z]+)\(([^)]*)\):[[:space:]](.*)$'
COMMIT_RE_PLAIN='^([a-zA-Z]+):[[:space:]](.*)$'

repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"

freeze_before=""
rebuild=false
target=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --freeze-before)
      [[ $# -ge 2 ]] || { usage; exit 1; }
      freeze_before="$2"
      shift 2
      ;;
    --freeze-before=*)
      freeze_before="${1#*=}"
      shift
      ;;
    --rebuild)
      rebuild=true
      shift
      ;;
    dashboard|services|common|battery_soc_core|ha-integration|all)
      if [[ -n "$target" ]]; then
        usage
        exit 1
      fi
      target="$1"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 1
      ;;
  esac
done
target="${target:-all}"

freeze_major=""
freeze_minor=""
if [[ -n "$freeze_before" ]]; then
  if [[ "$freeze_before" =~ $FREEZE_VERSION_RE ]]; then
    freeze_major="${BASH_REMATCH[1]}"
    freeze_minor="${BASH_REMATCH[2]}"
  else
    echo "Ungueltige --freeze-before Version: '$freeze_before' (erwartet z.B. v1.2 oder 1.2.0)" >&2
    exit 1
  fi
fi

reset_bucket() {
  GBUCKET=()
  local t
  for t in "${TYPE_ORDER[@]}"; do GBUCKET[$t]=""; done
}

# Baut den akkumulierten GBUCKET-Inhalt der laufenden Gruppe zu einem
# Markdown-Abschnitt zusammen und haengt ihn (samt Major/Minor-Metadaten
# fuer --freeze-before) an SECTIONS an.
flush_group() {
  [[ -z "$group_date" ]] && return

  local heading
  if [[ -n "$group_unversioned" ]]; then
    heading="## Unversioniert (bis ${group_date})"
  elif [[ -n "$group_tag" ]]; then
    heading="## ${group_tag} (${group_date})"
  else
    heading="## ${group_version} (${group_date})"
  fi

  local block="${heading}"$'\n\n'
  local t any=false
  for t in "${TYPE_ORDER[@]}"; do
    if [[ -n "${GBUCKET[$t]}" ]]; then
      any=true
      block+="### ${LABELS[$t]}"$'\n\n'"${GBUCKET[$t]}"$'\n'
    fi
  done
  [[ "$any" == true ]] || block+="_No changes yet._"$'\n\n'

  SECTIONS+=("$block")
  SECTION_MAJOR+=("$group_major")
  SECTION_MINOR+=("$group_minor")
  SECTION_UNVER+=("$group_unversioned")
}

classify_and_append() {
  local hash="$1" subject="$2"
  local type="other" scope="" rest="$subject" raw_type=""
  if [[ "$subject" =~ $COMMIT_RE_SCOPED ]]; then
    raw_type="${BASH_REMATCH[1],,}"
    scope="${BASH_REMATCH[2]}"
    rest="${BASH_REMATCH[3]}"
    [[ -n "${LABELS[$raw_type]:-}" ]] && type="$raw_type"
  elif [[ "$subject" =~ $COMMIT_RE_PLAIN ]]; then
    raw_type="${BASH_REMATCH[1],,}"
    rest="${BASH_REMATCH[2]}"
    [[ -n "${LABELS[$raw_type]:-}" ]] && type="$raw_type"
  fi
  local entry="- "
  [[ -n "$scope" ]] && entry+="**${scope}:** "
  entry+="${rest} (${hash})"
  GBUCKET[$type]+="${entry}"$'\n'
}

# "(hash)"-Referenzen aus einem Markdown-Block ziehen (7-40 Hex in Klammern).
block_hashes() {
  grep -oE '\([0-9a-f]{7,40}\)' <<< "$1" | tr -d '()'
}

# 0, wenn der Block mind. eine "(hash)"-Referenz hat und Git jede davon in der
# aktuellen Historie dieser Komponente noch kennt (HIST_HASHES) - der Block
# laesst sich dann aus der Commit-Historie neu bauen. Sonst 1 (einfrieren).
# HIST_HASHES wird von generate_one als 'local -A' bereitgestellt.
block_reproducible() {
  local h had=false
  while IFS= read -r h; do
    [[ -z "$h" ]] && continue
    had=true
    [[ -n "${HIST_HASHES[$h]:-}" ]] || return 1
  done < <(block_hashes "$1")
  $had
}

# 0, wenn jede "(hash)"-Referenz des neu gebauten Abschnitts ($1) bereits im
# eingefrorenen Text ($2) steht (dann nicht noch einmal voranstellen).
section_covered_by() {
  local h had=false
  while IFS= read -r h; do
    [[ -z "$h" ]] && continue
    had=true
    grep -qF "($h)" <<< "$2" || return 1
  done < <(block_hashes "$1")
  $had
}

# Generiert die CHANGELOG.md einer einzelnen Komponente (dashboard|services|common).
generate_one() {
  local comp="$1"
  # out_dir_prefix: aktueller Pfad, in den die CHANGELOG.md geschrieben wird.
  # history_prefixes: alle Pfade (inkl. fruehere, durch Umbenennung
  # verlassene), ueber die die Commit-Historie der Komponente laeuft -
  # noetig, damit ein "git mv"/Package-Rename (z.B. werkstatt_iot_common ->
  # energy_node_common) die Historie nicht abschneidet.
  # version_file_candidates: VERSION-Datei an jedem dieser Pfade, in der
  # Reihenfolge geprueft, in der sie fuer einen Commit existieren kann.
  local out_dir_prefix
  # min_freeze: fest hinterlegte, permanente Freeze-Untergrenze fuer diese
  # Komponente (leer = keine). Schuetzt von Hand geschriebene Alt-Historie
  # dauerhaft davor, beim naechsten Lauf ohne --freeze-before (z.B. via
  # "all") platt zu einer Section verschmolzen zu werden - siehe
  # ha-integration unten: deren CHANGELOG.md enthielt vor der Umstellung auf
  # automatische Generierung mehrere von Hand geschriebene Abschnitte
  # innerhalb derselben Minor-Version (v0.1.0-v0.1.4).
  local -a history_prefixes=() version_file_candidates=() exclude_prefixes=()
  local min_freeze=""
  case "$comp" in
    dashboard)
      out_dir_prefix="dashboard/"
      history_prefixes=("dashboard/")
      version_file_candidates=("dashboard/VERSION")
      ;;
    services)
      out_dir_prefix="services/"
      history_prefixes=("services/")
      version_file_candidates=("services/VERSION")
      exclude_prefixes=("libs/energy_node_common/" "src/werkstatt_iot_common/")
      ;;
    common)
      out_dir_prefix="libs/energy_node_common/"
      history_prefixes=("libs/energy_node_common/" "src/werkstatt_iot_common/")
      version_file_candidates=("libs/energy_node_common/VERSION" "src/werkstatt_iot_common/VERSION")
      ;;
    battery_soc_core)
      out_dir_prefix="libs/battery_soc_core/"
      history_prefixes=("libs/battery_soc_core/")
      version_file_candidates=("libs/battery_soc_core/VERSION")
      ;;
    ha-integration)
      out_dir_prefix="integrations/homeassistant/"
      history_prefixes=("integrations/homeassistant/")
      version_file_candidates=("integrations/homeassistant/custom_components/battery_soc/manifest.json")
      # v0.1.0-v0.1.4 wurden von Hand als 5 getrennte Abschnitte geschrieben,
      # bevor diese Komponente ab v0.2.0 auf automatische Generierung
      # umgestellt wurde - dauerhaft eingefroren, siehe Kommentar oben.
      min_freeze="v0.2"
      ;;
  esac

  # Effektive Freeze-Grenze: das groessere von --freeze-before (falls vom
  # Aufrufer gesetzt) und der Komponenten-Untergrenze min_freeze.
  local eff_major="" eff_minor=""
  if [[ -n "$min_freeze" ]]; then
    [[ "$min_freeze" =~ $FREEZE_VERSION_RE ]]
    eff_major="${BASH_REMATCH[1]}"
    eff_minor="${BASH_REMATCH[2]}"
  fi
  if [[ -n "$freeze_before" ]]; then
    if [[ -z "$eff_major" ]] || (( freeze_major > eff_major || (freeze_major == eff_major && freeze_minor > eff_minor) )); then
      eff_major="$freeze_major"
      eff_minor="$freeze_minor"
    fi
  fi
  local freeze_active=false
  [[ -n "$eff_major" ]] && freeze_active=true

  local changelog="${repo_root}/${out_dir_prefix}CHANGELOG.md"

  # auto_freeze (Standard, sofern nicht --rebuild): Abschnitte der bestehenden
  # CHANGELOG.md, deren Commit-Referenzen die aktuelle Commit-Historie nicht
  # mehr hergibt (von Hand gepflegte / aus dem Vorgaenger-Repo uebernommene
  # Alt-Historie - beim History-freien Fork faellt hier alles Vor-Fork-Wissen
  # rein), werden unveraendert eingefroren. Alles, was Git noch reproduzieren
  # kann, wird wie gehabt neu aus der Historie gebaut. So kann ein Lauf die
  # Alt-Historie nicht mehr plattmachen, bleibt aber idempotent.
  local auto_freeze=true
  $rebuild && auto_freeze=false

  local -A GBUCKET=()
  local -A HIST_HASHES=()   # alle %h dieser Komponente aus der Commit-Historie
  local group_unversioned="" group_major="" group_minor=""
  local group_version="" group_tag="" group_date=""
  local SECTIONS=() SECTION_MAJOR=() SECTION_MINOR=() SECTION_UNVER=()

  reset_bucket

  local pathspec=("--" "${history_prefixes[@]}")
  local ex
  for ex in "${exclude_prefixes[@]}"; do
    pathspec+=(":(exclude)${ex}")
  done

  # The version-bump workflow's own housekeeping commits are skipped (see the
  # --invert-grep below) so a CHANGELOG.md never lists the commits that wrote
  # it or the patch bump that rode along.
  local prev_tagged=false
  local first=true
  local hash subject
  while IFS=$'\t' read -r hash subject; do
    [[ -z "$hash" ]] && continue
    HIST_HASHES[$hash]=1

    local local_version=""
    local raw vf
    for vf in "${version_file_candidates[@]}"; do
      if raw="$(git -C "$repo_root" show "${hash}:${vf}" 2>/dev/null)"; then
        if [[ "$vf" == *.json ]]; then
          # manifest.json traegt eine nackte Semver (HACS/hassfest-Pflicht) -
          # fuer die einheitliche "vX.Y.Z"-Ueberschrift hier mit "v" versehen.
          local json_version
          json_version="$(python3 -c 'import json,sys
try:
    print(json.load(sys.stdin).get("version", ""))
except Exception:
    pass' <<< "$raw")"
          [[ -n "$json_version" ]] && local_version="v${json_version}"
        else
          local_version="$(tr -d '[:space:]' <<< "$raw")"
        fi
        break
      fi
    done

    local local_unversioned=""
    local local_major="" local_minor=""
    if [[ "$local_version" =~ $VERSION_RE ]]; then
      local_major="${BASH_REMATCH[1]}"
      local_minor="${BASH_REMATCH[2]}"
    else
      local_unversioned=1
    fi

    local local_tag
    local_tag="$(git -C "$repo_root" tag --points-at "$hash" | head -n1)"

    local need_new_group=false
    if $first; then
      need_new_group=true
    elif $prev_tagged; then
      need_new_group=true
    elif [[ -n "$local_unversioned" || -n "$group_unversioned" ]]; then
      [[ "$local_unversioned" != "$group_unversioned" ]] && need_new_group=true
    elif [[ "$local_major" != "$group_major" || "$local_minor" != "$group_minor" ]]; then
      need_new_group=true
    fi

    if $need_new_group; then
      flush_group
      reset_bucket
      group_unversioned="$local_unversioned"
      group_major="$local_major"
      group_minor="$local_minor"
    fi

    classify_and_append "$hash" "$subject"
    group_version="$local_version"
    group_tag="$local_tag"
    group_date="$(git -C "$repo_root" log -1 --format=%ad --date=short "$hash")"

    prev_tagged=false
    [[ -n "$local_tag" ]] && prev_tagged=true
    first=false
  done < <(git -C "$repo_root" log --no-merges --reverse --pretty=format:'%h%x09%s' \
    --invert-grep \
    --grep='^chore(release): bump component versions$' \
    --grep='^docs(changelog): ' \
    "${pathspec[@]}"; printf '\n')

  flush_group

  # Bestehende CHANGELOG.md in Abschnitte ("## ...") zerlegen und - top-down -
  # den ersten Abschnitt suchen, ab dem eingefroren wird. Ausloeser:
  #   * auto_freeze: der Abschnitt ist aus der aktuellen Commit-Historie nicht
  #     reproduzierbar (mind. eine "(hash)"-Referenz fehlt in HIST_HASHES, oder
  #     der Abschnitt fuehrt gar keine Hashes) - typisch fuer von Hand
  #     gepflegte / vor dem History-freien Fork geschriebene Abschnitte.
  #   * --freeze-before / min_freeze: Abschnitt "## vX.Y." mit Major.Minor <
  #     eff_major.eff_minor, oder "## Unversioniert ...".
  # Ab dem Treffer wird der Rest der Datei unveraendert als frozen_tail
  # uebernommen; alles darueber baut sich neu aus der Commit-Historie auf.
  # Kein Treffer (frische Datei, oder Git kennt noch jeden Abschnitt) ->
  # frozen_tail bleibt leer, es wird komplett frisch generiert.
  local frozen_tail=""
  if { $auto_freeze || $freeze_active; } && [[ -f "$changelog" ]]; then
    local old_blocks_raw=()
    mapfile -d '' -t old_blocks_raw < <(awk 'BEGIN{RS="\n## "} NR>1{printf "## %s%c", $0, 0}' "$changelog")
    local n=${#old_blocks_raw[@]}
    local i old_block old_heading old_major old_minor
    local found=false
    for (( i = 0; i < n; i++ )); do
      old_block="${old_blocks_raw[$i]}"
      # awk's RS match verschluckt beim Splitten eine Newline an der
      # Abschnittsgrenze - hier zwischen Bloecken wieder ergaenzen, sonst
      # fehlt die Leerzeile vor der naechsten Ueberschrift. Der letzte Block
      # (laeuft bis EOF) hatte keine konsumierte Newline, also unveraendert.
      (( i < n - 1 )) && old_block+=$'\n'
      if ! $found; then
        old_heading="${old_block%%$'\n'*}"
        if $auto_freeze && ! block_reproducible "$old_block"; then
          found=true
        elif [[ -n "$eff_major" && "$old_heading" =~ ^\#\#\ v([0-9]+)\.([0-9]+)\. ]]; then
          old_major="${BASH_REMATCH[1]}"
          old_minor="${BASH_REMATCH[2]}"
          if (( old_major < eff_major || (old_major == eff_major && old_minor < eff_minor) )); then
            found=true
          fi
        elif [[ -n "$eff_major" && "$old_heading" == "## Unversioniert "* ]]; then
          found=true
        fi
      fi
      $found && frozen_tail+="$old_block"
    done
  fi

  local final_blocks=()
  local idx=${#SECTIONS[@]}
  while (( idx > 0 )); do
    (( idx-- ))
    if [[ -n "$frozen_tail" ]]; then
      local is_new=true
      if [[ "${SECTION_UNVER[$idx]}" == "1" ]]; then
        is_new=false
      elif [[ -n "$eff_major" && -n "${SECTION_MAJOR[$idx]}" ]] \
        && (( SECTION_MAJOR[idx] < eff_major || (SECTION_MAJOR[idx] == eff_major && SECTION_MINOR[idx] < eff_minor) )); then
        is_new=false
      elif section_covered_by "${SECTIONS[$idx]}" "$frozen_tail"; then
        # jede "(hash)"-Referenz dieses neu gebauten Abschnitts steckt schon
        # im eingefrorenen Teil -> nicht doppelt voranstellen (Idempotenz).
        is_new=false
      fi
      $is_new || continue
    fi
    final_blocks+=("${SECTIONS[$idx]}")
  done

  {
    echo "# Changelog"
    echo
    local b
    for b in "${final_blocks[@]}"; do
      printf '%s' "$b"
    done
    [[ -n "$frozen_tail" ]] && printf '%s' "$frozen_tail"
  } > "$changelog"

  echo "$(basename "$0"): $changelog aktualisiert (${#final_blocks[@]} Abschnitte)" >&2
}

if [[ "$target" == "all" ]]; then
  for c in "${ALL_TARGETS[@]}"; do
    generate_one "$c"
  done
else
  generate_one "$target"
fi

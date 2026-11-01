package unitlogs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// journalScript ist das Remote-Skript des Journal-Stroms. Der Wächter beendet
// journalctl, sobald stdin zugeht (Verbindung oder Diagnose geschlossen), damit
// der Prozess auf dem Pi nicht weiterlaeuft. exec 3<&0 ist noetig, weil eine
// Hintergrundliste ohne Job-Control sonst /dev/null als stdin bekaeme.
const journalScript = `S=; if sudo -n true 2>/dev/null; then S='sudo -n'; fi
exec 3<&0
$S journalctl --follow --output=json --no-pager %s </dev/null &
p=$!
( cat <&3 >/dev/null 2>&1; kill $p 2>/dev/null ) >/dev/null 2>&1 &
wait $p`

// statusProperties sind die Felder, die StatusCommand abfragt.
const statusProperties = "LoadState,ActiveState,SubState,Result,ExecMainStatus,NRestarts,MainPID,ActiveEnterTimestamp"

// JournalCommand baut das Remote-Skript fuer den Journal-Strom. Mit
// AfterCursor beginnt der Strom nach dem Cursor, sonst mit den letzten
// Backlog-Zeilen.
func JournalCommand(req hostapi.UnitLogsRequest) string {
	var args []string
	if req.AfterCursor != "" {
		args = append(args, "--after-cursor="+transport.ShellQuote(req.AfterCursor))
	} else {
		args = append(args, "--lines="+strconv.Itoa(req.Backlog))
	}
	for _, unit := range req.Units {
		args = append(args, "-u "+transport.ShellQuote(unit))
	}
	return fmt.Sprintf(journalScript, strings.Join(args, " "))
}

// StatusCommand fragt den Zustand einer Unit mit systemctl show ab.
func StatusCommand(unit string) string {
	return "systemctl show --timestamp=unix --property=" + statusProperties +
		" -- " + transport.ShellQuote(unit)
}

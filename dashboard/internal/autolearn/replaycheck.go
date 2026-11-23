package autolearn

import (
	"context"
	"time"

	"github.com/Developer-Simon/energy-node-dashboard/internal/sun"
)

// ReplayVerdict is the replay check of a suggestion. Base is the real
// situation without the suggestion, Alt the situation with it, both over the
// same minutes. Estimate is set when stretched quarter hours took part.
type ReplayVerdict struct {
	Base, Alt ReplayResult
	Estimate  bool
}

// ReplayCheck describes what a suggestion would change. Rule is the proposed
// rule (a Rule or raw JSON), Target the device it switches. Benefit turns the
// two replays into the benefit of the suggestion, with the proposal first.
type ReplayCheck struct {
	Rule    any
	Target  ReplayTarget
	Benefit func(alt, base ReplayResult) Benefit
}

// replayBaseRule never fires. Replayed with the real switch state of the
// target, it reproduces the real situation.
var replayBaseRule = Rule{ID: "replay-base", Enabled: false}

// EvaluateReplay replays the proposal of chk over the last days days. It
// returns a nil verdict and no benefit when there are no minutes to replay.
// An ErrNotReplayable of the proposed rule is passed on, so the caller can
// keep the candidate without a replay and log the reason.
func EvaluateReplay(ctx context.Context, in *Input, chk ReplayCheck, days int, loc *time.Location, site sun.Location) (*ReplayVerdict, Benefit, error) {
	minutes, estimate := ReplayDays(in, days)
	if len(minutes) == 0 {
		return nil, Benefit{}, nil
	}
	real := RealState(in, chk.Target.Device, minutes)
	base, err := Replay(ctx, replayBaseRule, chk.Target, minutes, real, loc, site)
	if err != nil {
		return nil, Benefit{}, err
	}
	alt, err := Replay(ctx, chk.Rule, chk.Target, minutes, real, loc, site)
	if err != nil {
		return nil, Benefit{}, err
	}
	base.Estimate, alt.Estimate = estimate, estimate
	var benefit Benefit
	if chk.Benefit != nil {
		benefit = chk.Benefit(alt, base)
	}
	return &ReplayVerdict{Base: base, Alt: alt, Estimate: estimate}, benefit, nil
}

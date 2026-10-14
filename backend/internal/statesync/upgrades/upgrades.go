// Package upgrades records when each validator came back after a software
// upgrade halted the chain (display only: nothing scores it).
//
// At a passed upgrade's plan height U, the old binary decides block U and
// stops while executing it (the upgrade module's PreBlocker panics), so
// consensus waits at U+1 until two thirds of the voting power run the new
// binary. Block U+1's header carries the time block U was decided (the
// halt); block U+2's the time U+1 was decided (the resume). Checked on
// mainnet: v8.1.0 (U 22,551,810) and v8.1.1 (U 23,855,000) both show the
// gap between U+1 and U+2.
//
// Two sources, because the commits alone cannot tell the early validators
// from the late ones: everybody who signed U+1's commit looks equally on
// time, the one that tipped the chain over two thirds hours later included.
//
//   - The halt Watch (live only, the precise one) polls the local node's
//     /consensus_state while the chain waits at U+1: a validator that is back
//     votes at U+1, round after round, and is recorded the first time a poll
//     sees it (back_source halt_vote).
//   - The upgrade_recovery Set reads /commit from U+1 onwards, through the
//     local node or the archive: a member without a halt vote is back when
//     the first block it signed was decided (back_source commit). It is the
//     only source for the upgrades before the explorer ran; the members that
//     signed U+1 are then all back at the resume.
package upgrades

import (
	"context"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
)

// Sources of a member's back_time.
const (
	SourceHaltVote = "halt_vote"
	SourceCommit   = "commit"
)

// Upgrade is an explorer.upgrades row.
type Upgrade struct {
	// Height is the plan height U.
	Height     int64
	ProposalID int64
	Name       string
	// HaltTime and ResumeTime are nil until the commits of U+1 and U+2
	// were read.
	HaltTime   *time.Time
	ResumeTime *time.Time
	// Watched: the halt watch saw the halt's votes.
	Watched bool
	// ScannedTo is the last height whose commit was read, 0 for none.
	ScannedTo int64
	// Resumed: blocks U+1 and U+2 are indexed, the chain came back from
	// the halt in the explorer's blocks.
	Resumed bool
}

// HaltHeight is the height consensus waits at during the upgrade's halt.
func (u Upgrade) HaltHeight() int64 { return u.Height + 1 }

// Member is an explorer.upgrade_recovery row as the set reads it.
type Member struct {
	ConsensusAddress string
	// Back: the member has a back_time.
	Back bool
}

// HaltVote is a member the halt watch saw voting.
type HaltVote struct {
	ConsensusAddress string
	// BackTime is the poll that saw it, VoteTime its own time on its
	// earliest vote.
	BackTime time.Time
	VoteTime time.Time
}

// CommitBack is a member back by the first block it signed.
type CommitBack struct {
	ConsensusAddress string
	// BackTime is when that block was decided: the time of the next block.
	BackTime time.Time
	// VoteTime is its own time on the signature.
	VoteTime          time.Time
	FirstSignedHeight int64
	// BlocksNeeded is how many heights after U+1 it signed first.
	BlocksNeeded int64
}

// Scan is what one read of commits found for an upgrade.
type Scan struct {
	Height int64
	// HaltTime and ResumeTime are set by the scan that read U+1.
	HaltTime   *time.Time
	ResumeTime *time.Time
	SetSize    int
	Backs      []CommitBack
	// ScannedTo is the last height whose commit was read.
	ScannedTo int64
	// Complete stamps computed_at.
	Complete bool
}

// Store reads and writes the upgrades and their members.
type Store interface {
	// Discover adds the passed software upgrades it does not hold yet, by
	// the plan of their message.
	Discover(ctx context.Context) error
	// Pending lists the upgrades not computed yet, by height.
	Pending(ctx context.Context) ([]Upgrade, error)
	// Upgrade returns the upgrade planned at height, false when none.
	Upgrade(ctx context.Context, height int64) (Upgrade, bool, error)
	// Tip is the live indexer's cursor, false before the first block.
	Tip(ctx context.Context) (int64, bool, error)
	// Members lists the upgrade's members.
	Members(ctx context.Context, height int64) ([]Member, error)
	// AddMembers writes the set at U+1, keeping what is recorded of the
	// members it already holds (their operator is refreshed).
	AddMembers(ctx context.Context, height int64, set []activeset.Member) error
	// HaltVotes records the members first seen voting during the halt
	// (never over an earlier halt vote) and marks the upgrade watched.
	HaltVotes(ctx context.Context, height int64, votes []HaltVote) error
	// SaveScan writes a scan in one transaction: the members without a
	// back_time it found back, the times, the progress and seconds_back.
	SaveScan(ctx context.Context, s Scan) error
	// Reset forgets what the commits gave (the halt votes stay) for a
	// recompute.
	Reset(ctx context.Context, height int64) error
}

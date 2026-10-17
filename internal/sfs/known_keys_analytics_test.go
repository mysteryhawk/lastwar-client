package sfs

// Read-only leaderboard scope/paging parameters from the historical Lua
// handlers. These contain no player identities or authentication material.
func init() {
	for _, k := range []string{"global", "event_id", "rank_type", "pageSize", "grabTimes", "startRank", "count", "start", "end"} {
		knownNonSensitiveSFSKeys[k] = true
	}
}

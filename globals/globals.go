package globals

var (
	useLeagues bool
	league     int = -1 // types.InvalidLeague
	nodeId     int = -1 // types.InvalidNodeId
	// TODO make a separate package for these constants
	// avoid using types package to prevent circular loops in imports

	StartTimestamp int64 = 0
)

func UseLeagues() bool {
	return useLeagues
}

func SetUseLeagues(val bool) {
	useLeagues = val
}

func League() int {
	return league
}

func SetLeague(val int) {
	league = val
}

func NodeId() int {
	return nodeId
}

func SetNodeId(val int) {
	nodeId = val
}


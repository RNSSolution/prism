package globals

import (
	"github.com/ColorPlatform/prism/types"
	"github.com/ColorPlatform/prism/libs/log"
)

var (
	useLeagues	bool
	league      int = types.InvalidLeague
	nodeId      int = types.InvalidNodeId
	logger      log.Logger
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

func SetLogger(log log.Logger) {
	logger = log
}

func Logger() log.Logger {
	return logger 
}

//------------------------------------------------------------
// Global info about leagues
var (
	leagueDoc *types.LeaguesDoc
)

func DefineLeagues(doc *types.LeaguesDoc) {
	leagueDoc = doc
}

func GetLeagues() *types.LeaguesDoc {
	return leagueDoc
}


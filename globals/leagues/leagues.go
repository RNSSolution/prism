package leagues

import (
	"github.com/ColorPlatform/prism/types"
)

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

package types

import (
	"fmt"
	"io/ioutil"
	"bytes"

	cmn "github.com/ColorPlatform/prism/libs/common"
	"github.com/ColorPlatform/prism/crypto"
)


//------------------------------------------------------------
// core types for a league definition
// NOTE: any changes to the genesis definition should
// be reflected in the documentation

const (
	InvalidLeague int = -1
	InvalidNodeId int = -1
)

//------------------------------------------------------------
// League-related data in P2P.Peer
type LeagueNodeInfo struct {
	League int `json:"league"`
	NodeId int `json:"node_id"`
}


//------------------------------------------------------------
// Information about peers in Genesis doc
type LeaguePeer struct {
	League       int            `json:"league"`
	NodeId       int            `json:"node_id"`
	PubKey       crypto.PubKey  `json:"pub_key"`
	Hostname     string         `json:"hostname"`
}

// Defines list of peers belonging to leagues
type Leagues []LeaguePeer

func MakeEmptyLeagues() (Leagues) {
	return make([]LeaguePeer, 0)
}

// LeaguesDoc defines the initial conditions for a tendermint blockchain, in particular its validator set.
type LeaguesDoc struct {
	Leagues int          `json:"leagues"`
	Peers   []LeaguePeer `json:"peers,omitempty"`
}

func (doc * LeaguesDoc) GetPeers(league int) []LeaguePeer {
	if league < 0 || league >= doc.Leagues {
		return nil
	}
	var res []LeaguePeer
	for _,peer := range(doc.Peers) {
		if peer.League == league {
			res = append(res, peer)
		}
	}
	return res
}

func (doc * LeaguesDoc) GetPeer(addr crypto.Address) (LeaguePeer, error) {
	binAddr := addr.Bytes()
	for _, peer := range(doc.Peers) {
		if 0 == bytes.Compare(binAddr, peer.PubKey.Address().Bytes()) {
			return peer, nil
		}
	}
	return LeaguePeer{}, cmn.NewError("Failed to find league peer by address: %v", addr)
}

//------------------------------------------------------------
// Make leagues topology from file

// SaveAs is a utility method for saving LeaguesDoc as a JSON file.
func (doc *LeaguesDoc) SaveAs(file string) error {
	docBytes, err := cdc.MarshalJSONIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return cmn.WriteFile(file, docBytes, 0644)
}

// LeaguesDocFromJSON unmarshalls JSON data into a LeaguesDoc.
func LeaguesDocFromJSON(jsonBlob []byte) (*LeaguesDoc, error) {
	doc := LeaguesDoc{}
	err := cdc.UnmarshalJSON(jsonBlob, &doc)
	if err != nil {
		return nil, err
	}

	if err := doc.ValidateAndComplete(); err != nil {
		return nil, err
	}

	return &doc, err
}

// LeaguesDocFromFile reads JSON data from a file and unmarshalls it into a LeaguesDoc.
func LeaguesDocFromFile(leaguesDocFile string) (*LeaguesDoc, error) {
	jsonBlob, err := ioutil.ReadFile(leaguesDocFile)
	if err != nil {
		return nil, cmn.ErrorWrap(err, "Couldn't read LeaguesDoc file")
	}
	doc, err := LeaguesDocFromJSON(jsonBlob)
	if err != nil {
		return nil, cmn.ErrorWrap(err, fmt.Sprintf("Error reading LeaguesDoc at %v", leaguesDocFile))
	}
	return doc, nil
}

// ValidateAndComplete checks that all necessary fields are present
// and fills in defaults for optional fields left empty
// TODO: Complete checks
func (doc *LeaguesDoc) ValidateAndComplete() error {
	if doc.Leagues <0 {
		return cmn.NewError("Incorrect number of leagues in leagues file: %d", doc.Leagues)
	}
	if doc.Leagues == 0 && len(doc.Peers) > 0 {
		return cmn.NewError("Too many nodes specified: %d, expected 0", len(doc.Peers))
	}
	for _, n := range doc.Peers {
		if n.League < 0 || n.League >= doc.Leagues {
			return cmn.NewError("Unexpected league for node %v: expected from 0 to %d", n, doc.Leagues - 1)
		}
		// TODO: Check that all leagues have equal number of nodes
		// TODO: Check that there are no node duplicates 
	}

	return nil
}


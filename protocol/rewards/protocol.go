// Copyright (c) 2019 IoTeX
// This is an alpha (internal) release and is not suitable for production. This source code is provided 'as is' and no
// warranties are given as to title or non-infringement, merchantability or fitness for purpose and, to the extent
// permitted by law, all liability for your use of the code is disclaimed. This source code is governed by Apache
// License 2.0 that can be found in the LICENSE file.

package rewards

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/golang/protobuf/proto"
	"github.com/iotexproject/go-pkgs/hash"
	"github.com/iotexproject/iotex-core/action"
	"github.com/iotexproject/iotex-core/action/protocol/poll"
	"github.com/iotexproject/iotex-core/action/protocol/rewarding/rewardingpb"
	"github.com/iotexproject/iotex-core/blockchain/block"
	"github.com/iotexproject/iotex-core/pkg/log"
	"github.com/iotexproject/iotex-core/pkg/util/byteutil"
	"github.com/iotexproject/iotex-election/pb/api"
	"github.com/iotexproject/iotex-proto/golang/iotexapi"
	"github.com/pkg/errors"

	"github.com/iotexproject/iotex-analytics/indexcontext"
	"github.com/iotexproject/iotex-analytics/protocol"
	s "github.com/iotexproject/iotex-analytics/sql"
)

const (
	// ProtocolID is the ID of protocol
	ProtocolID = "rewards"
	// RewardHistoryTableName is the table name of reward history
	RewardHistoryTableName = "reward_history"
	// AccountRewardViewName is the view name of account rewards
	AccountRewardViewName = "account_reward"
)

type (
	// RewardHistory defines the schema of "reward history" table
	RewardHistory struct {
		EpochNumber     uint64
		ActionHash      string
		RewardAddress   string
		CandidateName   string
		BlockReward     string
		EpochReward     string
		FoundationBonus string
	}

	// AccountReward defines the schema of "account reward" table
	AccountReward struct {
		EpochNumber     uint64
		CandidateName   string
		BlockReward     string
		EpochReward     string
		FoundationBonus string
	}
)

// Protocol defines the protocol of indexing blocks
type Protocol struct {
	Store                 s.Store
	NumDelegates          uint64
	NumCandidateDelegates uint64
	NumSubEpochs          uint64
	RewardAddrToName      map[string]string
}

// RewardInfo indicates the amount of different reward types
type RewardInfo struct {
	BlockReward     *big.Int
	EpochReward     *big.Int
	FoundationBonus *big.Int
}

// NewProtocol creates a new protocol
func NewProtocol(store s.Store, numDelegates uint64, numSubEpochs uint64) *Protocol {
	return &Protocol{Store: store, NumDelegates: numDelegates, NumSubEpochs: numSubEpochs}
}

// CreateTables creates tables
func (p *Protocol) CreateTables(ctx context.Context) error {
	// create reward history table
	if _, err := p.Store.GetDB().Exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s ([epoch_number] INT NOT NULL, "+
		"[action_hash] TEXT NOT NULL, [reward_address] TEXT NOT NULL, [candidate_name] TEXT NOT NULL, "+
		"[block_reward] BIGINT NOT NULL, [epoch_reward] BIGINT NOT NULL, [foundation_bonus] BIGINT NOT NULL)",
		RewardHistoryTableName)); err != nil {
		return err
	}

	if _, err := p.Store.GetDB().Exec(fmt.Sprintf("CREATE VIEW %s AS SELECT epoch_number, candidate_name, "+
		"SUM(block_reward) AS block_reward, SUM(epoch_reward) AS epoch_reward, SUM(foundation_bonus) AS foundation_bonus FROM %s GROUP BY epoch_number, candidate_name",
		AccountRewardViewName, RewardHistoryTableName)); err != nil {
		return err
	}

	return nil
}

// Initialize initializes rewards protocol
func (p *Protocol) Initialize(ctx context.Context, tx *sql.Tx, genesisCfg *protocol.GenesisConfig) error {
	return nil
}

// HandleBlock handles blocks
func (p *Protocol) HandleBlock(ctx context.Context, tx *sql.Tx, blk *block.Block) error {
	height := blk.Height()
	epochNumber := protocol.GetEpochNumber(p.NumDelegates, p.NumSubEpochs, height)
	indexCtx := indexcontext.MustGetIndexCtx(ctx)
	chainClient := indexCtx.ChainClient
	electionClient := indexCtx.ElectionClient
	// Special handling for epoch start height
	if height == protocol.GetEpochHeight(epochNumber, p.NumDelegates, p.NumSubEpochs) || p.RewardAddrToName == nil {
		if err := p.updateCandidateRewardAddress(chainClient, electionClient, height); err != nil {
			return errors.Wrapf(err, "failed to update candidates in epoch %d", epochNumber)
		}
	}

	grantRewardActs := make(map[hash.Hash256]bool)
	// log action index
	for _, selp := range blk.Actions {
		if _, ok := selp.Action().(*action.GrantReward); ok {
			grantRewardActs[selp.Hash()] = true
		}
	}
	// log receipt index
	for _, receipt := range blk.Receipts {
		if _, ok := grantRewardActs[receipt.ActionHash]; ok {
			// Parse receipt of grant reward
			rewardInfoMap, err := p.getRewardInfoFromReceipt(receipt)
			if err != nil {
				return errors.Wrap(err, "failed to get reward info from receipt")
			}
			// Update reward info in DB
			actionHash := hex.EncodeToString(receipt.ActionHash[:])
			if err := p.updateRewardHistory(tx, epochNumber, actionHash, rewardInfoMap); err != nil {
				return errors.Wrap(err, "failed to update epoch number and reward address to reward history table")
			}
		}
	}
	return nil
}

// getRewardHistory reads reward history
func (p *Protocol) getRewardHistory(actionHash string) ([]*RewardHistory, error) {
	db := p.Store.GetDB()

	getQuery := fmt.Sprintf("SELECT * FROM %s WHERE action_hash=?",
		RewardHistoryTableName)
	stmt, err := db.Prepare(getQuery)
	if err != nil {
		return nil, errors.Wrap(err, "failed to prepare get query")
	}

	rows, err := stmt.Query(actionHash)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute get query")
	}

	var rewardHistory RewardHistory
	parsedRows, err := s.ParseSQLRows(rows, &rewardHistory)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse results")
	}

	if len(parsedRows) == 0 {
		return nil, protocol.ErrNotExist
	}

	var rewardHistoryList []*RewardHistory
	for _, parsedRow := range parsedRows {
		rewards := parsedRow.(*RewardHistory)
		rewardHistoryList = append(rewardHistoryList, rewards)
	}
	return rewardHistoryList, nil
}

// getAccountReward reads account reward details
func (p *Protocol) getAccountReward(epochNumber uint64, candidateName string) (*AccountReward, error) {
	db := p.Store.GetDB()

	getQuery := fmt.Sprintf("SELECT * FROM %s WHERE epoch_number=? AND candidate_name=?",
		AccountRewardViewName)
	stmt, err := db.Prepare(getQuery)
	if err != nil {
		return nil, errors.Wrap(err, "failed to prepare get query")
	}

	rows, err := stmt.Query(epochNumber, candidateName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute get query")
	}

	var accountReward AccountReward
	parsedRows, err := s.ParseSQLRows(rows, &accountReward)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse results")
	}

	if len(parsedRows) == 0 {
		return nil, protocol.ErrNotExist
	}

	if len(parsedRows) > 1 {
		return nil, errors.New("only one row is expected")
	}

	return parsedRows[0].(*AccountReward), nil
}

// updateRewardHistory stores reward information into reward history table
func (p *Protocol) updateRewardHistory(tx *sql.Tx, epochNumber uint64, actionHash string, rewardInfoMap map[string]*RewardInfo) error {
	for rewardAddress, rewards := range rewardInfoMap {
		insertQuery := fmt.Sprintf("INSERT INTO %s (epoch_number, action_hash,reward_address,candidate_name,block_reward,epoch_reward,"+
			"foundation_bonus) VALUES (?, ?, ?, ?, CAST(? as BIGINT), CAST(? as BIGINT), CAST(? as BIGINT))", RewardHistoryTableName)
		blockReward := rewards.BlockReward.String()
		epochReward := rewards.EpochReward.String()
		foundationBonus := rewards.FoundationBonus.String()
		candidateName, ok := p.RewardAddrToName[rewardAddress]
		if !ok {
			return errors.New("cannot find candidate name by reward address")
		}
		if _, err := tx.Exec(insertQuery, epochNumber, actionHash, rewardAddress, candidateName, blockReward, epochReward, foundationBonus); err != nil {
			return err
		}
	}
	return nil
}

func (p *Protocol) getRewardInfoFromReceipt(receipt *action.Receipt) (map[string]*RewardInfo, error) {
	rewardInfoMap := make(map[string]*RewardInfo)
	for _, l := range receipt.Logs {
		rewardLog := &rewardingpb.RewardLog{}
		if err := proto.Unmarshal(l.Data, rewardLog); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal receipt data into reward log")
		}
		rewards, ok := rewardInfoMap[rewardLog.Addr]
		if !ok {
			rewardInfoMap[rewardLog.Addr] = &RewardInfo{
				BlockReward:     big.NewInt(0),
				EpochReward:     big.NewInt(0),
				FoundationBonus: big.NewInt(0),
			}
			rewards = rewardInfoMap[rewardLog.Addr]
		}
		amount, ok := big.NewInt(0).SetString(rewardLog.Amount, 10)
		if !ok {
			log.L().Fatal("Failed to convert reward amount from string to big int")
			return nil, errors.New("failed to convert reward amount from string to big int")
		}
		switch rewardLog.Type {
		case rewardingpb.RewardLog_BLOCK_REWARD:
			rewards.BlockReward.Add(rewards.BlockReward, amount)
		case rewardingpb.RewardLog_EPOCH_REWARD:
			rewards.EpochReward.Add(rewards.EpochReward, amount)
		case rewardingpb.RewardLog_FOUNDATION_BONUS:
			rewards.FoundationBonus.Add(rewards.FoundationBonus, amount)
		default:
			log.L().Fatal("Unknown type of reward")
		}
	}
	return rewardInfoMap, nil
}

func (p *Protocol) updateCandidateRewardAddress(
	chainClient iotexapi.APIServiceClient,
	electionClient api.APIServiceClient,
	height uint64,
) error {
	readStateRequest := &iotexapi.ReadStateRequest{
		ProtocolID: []byte(poll.ProtocolID),
		MethodName: []byte("GetGravityChainStartHeight"),
		Arguments:  [][]byte{byteutil.Uint64ToBytes(height)},
	}
	readStateRes, err := chainClient.ReadState(context.Background(), readStateRequest)
	if err != nil {
		return errors.Wrap(err, "failed to get gravity chain start height")
	}
	gravityChainStartHeight := byteutil.BytesToUint64(readStateRes.Data)

	getCandidatesRequest := &api.GetCandidatesRequest{
		Height: strconv.Itoa(int(gravityChainStartHeight)),
		Offset: uint32(0),
		Limit:  math.MaxUint32,
	}

	getCandidatesResponse, err := electionClient.GetCandidates(context.Background(), getCandidatesRequest)
	if err != nil {
		return errors.Wrap(err, "failed to get candidates from election service")
	}

	p.RewardAddrToName = make(map[string]string)
	for _, candidate := range getCandidatesResponse.Candidates {
		p.RewardAddrToName[candidate.RewardAddress] = candidate.Name
	}
	return nil
}

package pool

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	bolt "go.etcd.io/bbolt"
)

func testChainState(t *testing.T, db *bolt.DB) {
	var minedHeader wire.BlockHeader

	// Create mined work header.
	headerE := "07000000ff7d6ee2e7380b94e6215f933f55649a12f1f21da4cf" +
		"9601e90946eeb46f000066f27e7f98656bc19195a0a6d3a93d0d774b2e5" +
		"83f49f20f6fef11b38443e21a05bad23ac3f14278f0ad74a86ce08ca44d" +
		"05e0e2b0cd3bc91066904c311f482e01000000000000000000000000000" +
		"0004fa83b20204e0000000000002a000000a50300004348fa5db0350100" +
		"e7879320000000000000000000000000000000000000000000000000000" +
		"0000000000000"
	headerB, err := hex.DecodeString(headerE)
	if err != nil {
		t.Fatalf("unexpected encoding error %v", err)
	}
	err = minedHeader.FromBytes(headerB)
	if err != nil {
		t.Fatalf("unexpected deserialization error: %v", err)
	}
	minedHeaderB, err := minedHeader.Bytes()
	if err != nil {
		t.Fatalf("unexpected serialization error: %v", err)
	}

	payDividends := func(uint32) error {
		return nil
	}
	generatePayments := func(uint32, dcrutil.Amount) error {
		return nil
	}
	getBlock := func(*chainhash.Hash) (*wire.MsgBlock, error) {
		// Return a fake block.
		coinbase := wire.NewMsgTx()
		coinbase.AddTxOut(wire.NewTxOut(0, []byte{}))
		coinbase.AddTxOut(wire.NewTxOut(1, []byte{}))
		coinbase.AddTxOut(wire.NewTxOut(100, []byte{}))
		txs := make([]*wire.MsgTx, 1)
		txs[0] = coinbase
		block := &wire.MsgBlock{
			Header:       minedHeader,
			Transactions: txs,
		}
		return block, nil
	}
	pruneAcceptedWork := func(*bolt.DB, uint32) error {
		return nil
	}
	pendingPaymentsAtHeight := func(*bolt.DB, uint32) ([]*Payment, error) {
		return []*Payment{
			{Account: xID, Amount: dcrutil.Amount(100)},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	var confHeader wire.BlockHeader
	cCfg := &ChainStateConfig{
		DB:                      db,
		SoloPool:                false,
		PayDividends:            payDividends,
		GeneratePayments:        generatePayments,
		GetBlock:                getBlock,
		PruneAcceptedWork:       pruneAcceptedWork,
		PendingPaymentsAtHeight: pendingPaymentsAtHeight,
		Cancel:                  cancel,
		HubWg:                   new(sync.WaitGroup),
	}

	cs := NewChainState(cCfg)

	// Test pruneJobs.
	jobA, err := persistJob(db, "0700000093bdee7083c6e02147cf76724a685f0148636"+
		"b2faf96353d1cbf5c0a954100007991153ad03eb0e31ead44b75ebc9f760870098431d4e6"+
		"aa85e742cbad517ebd853b9bf059e8eeb91591e4a7d4005acc62e92bfd27b17309a5a41dd"+
		"24016428f0100000000000000000000003c000000dd742920204e00000000000038000000"+
		"66010000f171cc5d000000000000000000000000000000000000000000000000000000000"+
		"000000000000000000000008000000100000000000005a0", 56)
	if err != nil {
		t.Fatal(err)
	}

	jobB, err := persistJob(db, "0700000047e9425eabcf920eecf0c00c7bc46c6062049"+
		"071c59edcb0e55c0226690800005695619a600321a8389d1bee5b3a207efc81e05c111d38"+
		"1e960c8bf05ca336b55b528e9d5044c52aa0c713ae152f3fdb592f6ee82fa1776440ca72a"+
		"2fc9f77760100000000000000000000003c000000dd742920204e00000000000039000000"+
		"a6030000f171cc5d000000000000000000000000000000000000000000000000000000000"+
		"000000000000000000000008000000100000000000005a0", 57)
	if err != nil {
		t.Fatal(err)
	}

	// Prune jobs below height 58.
	err = cs.pruneJobs(58)
	if err != nil {
		t.Fatalf("pruneJobs error: %v", err)
	}

	// Ensure job A and B have been pruned.
	_, err = FetchJob(db, []byte(jobA.UUID))
	if err == nil {
		t.Fatalf("expected a value not found error: %v", err)
	}
	_, err = FetchJob(db, []byte(jobB.UUID))
	if err == nil {
		t.Fatalf("expected a value not found error: %v", err)
	}

	cCfg.HubWg.Add(1)
	go cs.handleChainUpdates(ctx)

	// Create the accepted work to be confirmed.
	work := NewAcceptedWork(
		"00007979602e13db87f6c760bbf27c137f4112b9e1988724bd245fb0bb7d1283",
		"00006fb4ee4609e90196cfa41df2f1129a64553f935f21e6940b38e7e26e7dff",
		42, xID, CPU)
	err = work.Create(cs.cfg.DB)
	if err != nil {
		t.Fatalf("unable to persist accepted work %v", err)
	}

	// Create associated job of the work to be confirmed.
	workE := "07000000ff7d6ee2e7380b94e6215f933f55649a12f1f21da4cf" +
		"9601e90946eeb46f000066f27e7f98656bc19195a0a6d3a93d0d774b2e5" +
		"83f49f20f6fef11b38443e21a05bad23ac3f14278f0ad74a86ce08ca44d" +
		"05e0e2b0cd3bc91066904c311f482e01000000000000000000000000000" +
		"0004fa83b20204e0000000000002a000000a50300004348fa5d00000000" +
		"00000000000000000000000000000000000000000000000000000000000" +
		"00000000000008000000100000000000005a0"
	job, err := NewJob(workE, 42)
	if err != nil {
		t.Fatalf("unable to create job %v", err)
	}
	err = job.Create(cs.cfg.DB)
	if err != nil {
		log.Errorf("failed to persist job %v", err)
		return
	}

	// Ensure a malformed connected block does not terminate the chain
	// state process.
	headerME := "0700000083127dbbb05f24bd248798e1b912417f137cf2bb60c7f" +
		"687db132e60797900007d79305d0f130f1371fe8e6a7d8aed721e969d45" +
		"e94ecbffe229c85e3fea8d2b8aba150b44486aa58f4c01a574e6a6c8657" +
		"66b3d3554fcc31d7061741e424158010000000000000000000a00000000" +
		"004fa83b20204e0000000000002b0000003a0f00004548fa5d70230000e" +
		"78793200000000000000000000000000000000000000000000000000000" +
		"0000000000"
	headerMB, err := hex.DecodeString(headerME)
	if err != nil {
		t.Fatalf("unexpected encoding error %v", err)
	}

	malformedMsg := &blockNotification{
		Header: headerMB,
		Done:   make(chan bool),
	}
	cs.connCh <- malformedMsg
	<-malformedMsg.Done

	// Create confirmation block header.
	headerE = "0700000083127dbbb05f24bd248798e1b912417f137cf2bb60c7f" +
		"687db132e60797900007d79305d0f130f1371fe8e6a7d8aed721e969d45" +
		"e94ecbffe229c85e3fea8d2b8aba150b44486aa58f4c01a574e6a6c8657" +
		"66b3d3554fcc31d7061741e424158010000000000000000000a00000000" +
		"004fa83b20204e0000000000002b0000003a0f00004548fa5d70230000e" +
		"78793200000000000000000000000000000000000000000000000000000" +
		"000000000000"
	headerB, err = hex.DecodeString(headerE)
	if err != nil {
		t.Fatalf("unexpected encoding error %v", err)
	}
	err = confHeader.FromBytes(headerB)
	if err != nil {
		t.Fatalf("unexpected deserialization error: %v", err)
	}
	confHeaderB, err := confHeader.Bytes()
	if err != nil {
		t.Fatalf("unexpected serialization error: %v", err)
	}

	minedMsg := &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- minedMsg
	<-minedMsg.Done
	confMsg := &blockNotification{
		Header: confHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- confMsg
	<-confMsg.Done

	// Ensure the accepted work is now confirmed mined.
	confirmedWork, err := FetchAcceptedWork(cs.cfg.DB, []byte(work.UUID))
	if err != nil {
		t.Fatalf("unable to confirm accepted work: %v", err)
	}
	if !confirmedWork.Confirmed {
		t.Fatalf("expected accepted work to be confirmed " +
			"after chain notifications")
	}

	discConfMsg := &blockNotification{
		Header: confHeaderB,
		Done:   make(chan bool),
	}
	cs.discCh <- discConfMsg
	<-discConfMsg.Done
	discMinedMsg := &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan bool),
	}
	cs.discCh <- discMinedMsg
	<-discMinedMsg.Done

	// Ensure the mined work is no longer confirmed mined.
	discMinedWork, err := FetchAcceptedWork(cs.cfg.DB, []byte(work.UUID))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if discMinedWork.Confirmed {
		t.Fatalf("disconnected mined work a height #%d should "+
			"be unconfirmed", discMinedWork.Height)
	}

	// Ensure a malformed disconnected block does not terminate the chain state
	// process.
	malformedMsg = &blockNotification{
		Header: headerMB,
		Done:   make(chan bool),
	}
	cs.discCh <- malformedMsg
	<-malformedMsg.Done

	// Ensure a dividend payment error does not teminate the chain state process.
	minedMsg = &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- minedMsg
	<-minedMsg.Done
	cs.cfg.PayDividends = func(uint32) error {
		return fmt.Errorf("unable to publish dividend transaction")
	}

	confMsg = &blockNotification{
		Header: confHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- confMsg
	<-confMsg.Done
	discConfMsg = &blockNotification{
		Header: confHeaderB,
		Done:   make(chan bool),
	}
	cs.discCh <- discConfMsg
	<-discConfMsg.Done
	cs.cfg.PayDividends = payDividends

	// Ensure the last work height can be updated.
	initialLastWorkHeight := cs.fetchLastWorkHeight()
	updatedLastWorkHeight := uint32(100)
	cs.setLastWorkHeight(updatedLastWorkHeight)
	lastWorkHeight := cs.fetchLastWorkHeight()
	if lastWorkHeight == initialLastWorkHeight {
		t.Fatalf("expected last work height to be %d, got %d",
			updatedLastWorkHeight, lastWorkHeight)
	}

	// Ensure the current work can be updated.
	initialCurrentWork := cs.fetchCurrentWork()
	updatedCurrentWork := headerE
	cs.setCurrentWork(updatedCurrentWork)
	currentWork := cs.fetchCurrentWork()
	if currentWork == initialCurrentWork {
		t.Fatalf("expected current work height to be %s, got %s",
			updatedCurrentWork, currentWork)
	}

	cancel()
	cs.cfg.HubWg.Wait()

	runChainState := func() {
		ctx, cancel = context.WithCancel(context.Background())
		cs = NewChainState(cCfg)
		cCfg.HubWg = new(sync.WaitGroup)
		cCfg.Cancel = cancel
		cCfg.HubWg.Add(1)
		go cs.handleChainUpdates(ctx)
	}

	runChainState()

	// Ensure a generate payment error terminates the chain state process.
	minedMsg = &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- minedMsg
	<-minedMsg.Done
	cs.cfg.GeneratePayments = func(uint32, dcrutil.Amount) error {
		return fmt.Errorf("unable to generate payments")
	}

	confMsg = &blockNotification{
		Header: confHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- confMsg
	<-confMsg.Done
	cs.cfg.GeneratePayments = generatePayments
	cs.cfg.HubWg.Wait()

	runChainState()

	// Ensure a prune accepted work error terminates the chain state process.
	minedMsg = &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- minedMsg
	<-minedMsg.Done
	cs.cfg.PruneAcceptedWork = func(*bolt.DB, uint32) error {
		return fmt.Errorf("unable to prune accepted work")
	}

	confMsg = &blockNotification{
		Header: confHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- confMsg
	<-confMsg.Done
	cs.cfg.PruneAcceptedWork = pruneAcceptedWork
	cs.cfg.HubWg.Wait()

	runChainState()

	// Ensure a pending payments at height error terminates the chain
	// state process.
	minedMsg = &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- minedMsg
	<-minedMsg.Done
	cs.cfg.PendingPaymentsAtHeight = func(*bolt.DB, uint32) ([]*Payment, error) {
		return nil, fmt.Errorf("unable to fetch pending payments")
	}

	discMinedMsg = &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan bool),
	}
	cs.discCh <- discMinedMsg
	<-discMinedMsg.Done
	cs.cfg.PendingPaymentsAtHeight = pendingPaymentsAtHeight
	cs.cfg.HubWg.Wait()

	runChainState()

	// Ensure a get block error terminates the chain state process.
	minedMsg = &blockNotification{
		Header: minedHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- minedMsg
	<-minedMsg.Done
	cs.cfg.GetBlock = func(*chainhash.Hash) (*wire.MsgBlock, error) {
		return nil, fmt.Errorf("unable to get block")
	}

	confMsg = &blockNotification{
		Header: confHeaderB,
		Done:   make(chan bool),
	}
	cs.connCh <- confMsg
	<-confMsg.Done
	cs.cfg.GetBlock = getBlock
	cs.cfg.HubWg.Wait()
}

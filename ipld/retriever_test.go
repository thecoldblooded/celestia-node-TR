package ipld

import (
	"context"
	"math/rand"
	"testing"
	"time"

	format "github.com/ipfs/go-ipld-format"
	mdutils "github.com/ipfs/go-merkledag/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/pkg/da"
	"github.com/tendermint/tendermint/pkg/wrapper"

	"github.com/celestiaorg/nmt"
	"github.com/celestiaorg/rsmt2d"
)

func init() {
	rand.Seed(time.Now().UnixNano()) // randomize quadrant fetching
}

func TestRetriever_Retrieve(t *testing.T) {
	NumWorkersLimit = 100            // limit the amount of workers for max square size case on CI
	rand.Seed(time.Now().UnixNano()) // otherwise, the quadrant sampling is deterministic in tests

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dag := mdutils.Mock()
	r := NewRetriever(dag)

	type test struct {
		name       string
		squareSize int
	}
	tests := []test{
		{"1x1(min)", 1},
		{"2x2(med)", 2},
		{"4x4(med)", 4},
		{"8x8(med)", 8},
		{"16x16(med)", 16},
		{"32x32(med)", 32},
		{"64x64(med)", 64},
		{"128x128(max)", MaxSquareSize},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// generate EDS
			shares := RandShares(t, tc.squareSize*tc.squareSize)
			in, err := AddShares(ctx, shares, dag)
			require.NoError(t, err)

			// limit with timeout, specifically retrieval
			ctx, cancel := context.WithTimeout(ctx, time.Minute*5) // the timeout is big for the max size which is long
			defer cancel()

			dah := da.NewDataAvailabilityHeader(in)
			out, err := r.Retrieve(ctx, &dah)
			require.NoError(t, err)
			assert.True(t, EqualEDS(in, out))
		})
	}
}

func TestRetriever_ByzantineError(t *testing.T) {
	const width = 8
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	dag := mdutils.Mock()
	shares := ExtractEDS(RandEDS(t, width))
	_, err := ImportShares(ctx, shares, dag)
	require.NoError(t, err)

	// corrupt shares so that eds erasure coding does not match
	copy(shares[14][8:], shares[15][8:])

	// import corrupted eds
	batchAdder := NewNmtNodeAdder(ctx, format.NewBatch(ctx, dag, format.MaxSizeBatchOption(batchSize(width*2))))
	tree := wrapper.NewErasuredNamespacedMerkleTree(uint64(width), nmt.NodeVisitor(batchAdder.Visit))
	attackerEDS, err := rsmt2d.ImportExtendedDataSquare(shares, DefaultRSMT2DCodec(), tree.Constructor)
	require.NoError(t, err)
	err = batchAdder.Commit()
	require.NoError(t, err)

	// ensure we rcv an error
	da := da.NewDataAvailabilityHeader(attackerEDS)
	r := NewRetriever(dag)
	_, err = r.Retrieve(ctx, &da)
	var errByz *ErrByzantine
	require.ErrorAs(t, err, &errByz)
}

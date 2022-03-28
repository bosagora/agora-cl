package blockchain

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	types "github.com/prysmaticlabs/eth2-types"
	"github.com/prysmaticlabs/prysm/beacon-chain/cache"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/blocks"
	testDB "github.com/prysmaticlabs/prysm/beacon-chain/db/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/forkchoice/protoarray"
	engine "github.com/prysmaticlabs/prysm/beacon-chain/powchain/engine-api-client/v1"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/beacon-chain/state/stategen"
	fieldparams "github.com/prysmaticlabs/prysm/config/fieldparams"
	"github.com/prysmaticlabs/prysm/config/params"
	"github.com/prysmaticlabs/prysm/encoding/bytesutil"
	v1 "github.com/prysmaticlabs/prysm/proto/engine/v1"
	ethpb "github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1"
	"github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1/block"
	"github.com/prysmaticlabs/prysm/proto/prysm/v1alpha1/wrapper"
	"github.com/prysmaticlabs/prysm/runtime/version"
	"github.com/prysmaticlabs/prysm/testing/assert"
	"github.com/prysmaticlabs/prysm/testing/require"
	"github.com/prysmaticlabs/prysm/testing/util"
	"github.com/prysmaticlabs/prysm/time/slots"
	logTest "github.com/sirupsen/logrus/hooks/test"
)

func Test_NotifyForkchoiceUpdate(t *testing.T) {
	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	altairBlk, err := wrapper.WrappedSignedBeaconBlock(util.NewBeaconBlockAltair())
	require.NoError(t, err)
	altairBlkRoot, err := altairBlk.Block().HashTreeRoot()
	require.NoError(t, err)
	bellatrixBlk, err := wrapper.WrappedSignedBeaconBlock(util.NewBeaconBlockBellatrix())
	require.NoError(t, err)
	bellatrixBlkRoot, err := bellatrixBlk.Block().HashTreeRoot()
	require.NoError(t, err)
	require.NoError(t, beaconDB.SaveBlock(ctx, altairBlk))
	require.NoError(t, beaconDB.SaveBlock(ctx, bellatrixBlk))
	fcs := protoarray.New(0, 0, [32]byte{'a'})
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stategen.New(beaconDB)),
		WithForkChoiceStore(fcs),
	}
	service, err := NewService(ctx, opts...)
	require.NoError(t, err)
	require.NoError(t, fcs.InsertOptimisticBlock(ctx, 0, [32]byte{}, [32]byte{}, params.BeaconConfig().ZeroHash, 0, 0))

	tests := []struct {
		name             string
		blk              block.BeaconBlock
		finalizedRoot    [32]byte
		newForkchoiceErr error
		errString        string
	}{
		{
			name:      "nil block",
			errString: "nil head block",
		},
		{
			name: "phase0 block",
			blk: func() block.BeaconBlock {
				b, err := wrapper.WrappedBeaconBlock(&ethpb.BeaconBlock{Body: &ethpb.BeaconBlockBody{}})
				require.NoError(t, err)
				return b
			}(),
		},
		{
			name: "altair block",
			blk: func() block.BeaconBlock {
				b, err := wrapper.WrappedBeaconBlock(&ethpb.BeaconBlockAltair{Body: &ethpb.BeaconBlockBodyAltair{}})
				require.NoError(t, err)
				return b
			}(),
		},
		{
			name: "not execution block",
			blk: func() block.BeaconBlock {
				b, err := wrapper.WrappedBeaconBlock(&ethpb.BeaconBlockBellatrix{
					Body: &ethpb.BeaconBlockBodyBellatrix{
						ExecutionPayload: &v1.ExecutionPayload{
							ParentHash:    make([]byte, fieldparams.RootLength),
							FeeRecipient:  make([]byte, fieldparams.FeeRecipientLength),
							StateRoot:     make([]byte, fieldparams.RootLength),
							ReceiptsRoot:  make([]byte, fieldparams.RootLength),
							LogsBloom:     make([]byte, fieldparams.LogsBloomLength),
							PrevRandao:    make([]byte, fieldparams.RootLength),
							BaseFeePerGas: make([]byte, fieldparams.RootLength),
							BlockHash:     make([]byte, fieldparams.RootLength),
						},
					},
				})
				require.NoError(t, err)
				return b
			}(),
		},
		{
			name: "happy case: finalized root is altair block",
			blk: func() block.BeaconBlock {
				b, err := wrapper.WrappedBeaconBlock(&ethpb.BeaconBlockBellatrix{
					Body: &ethpb.BeaconBlockBodyBellatrix{
						ExecutionPayload: &v1.ExecutionPayload{},
					},
				})
				require.NoError(t, err)
				return b
			}(),
			finalizedRoot: altairBlkRoot,
		},
		{
			name: "happy case: finalized root is bellatrix block",
			blk: func() block.BeaconBlock {
				b, err := wrapper.WrappedBeaconBlock(&ethpb.BeaconBlockBellatrix{
					Body: &ethpb.BeaconBlockBodyBellatrix{
						ExecutionPayload: &v1.ExecutionPayload{},
					},
				})
				require.NoError(t, err)
				return b
			}(),
			finalizedRoot: bellatrixBlkRoot,
		},
		{
			name: "forkchoice updated with optimistic block",
			blk: func() block.BeaconBlock {
				b, err := wrapper.WrappedBeaconBlock(&ethpb.BeaconBlockBellatrix{
					Body: &ethpb.BeaconBlockBodyBellatrix{
						ExecutionPayload: &v1.ExecutionPayload{},
					},
				})
				require.NoError(t, err)
				return b
			}(),
			newForkchoiceErr: engine.ErrAcceptedSyncingPayloadStatus,
			finalizedRoot:    bellatrixBlkRoot,
		},
		{
			name: "forkchoice updated with invalid block",
			blk: func() block.BeaconBlock {
				b, err := wrapper.WrappedBeaconBlock(&ethpb.BeaconBlockBellatrix{
					Body: &ethpb.BeaconBlockBodyBellatrix{
						ExecutionPayload: &v1.ExecutionPayload{},
					},
				})
				require.NoError(t, err)
				return b
			}(),
			newForkchoiceErr: engine.ErrInvalidPayloadStatus,
			finalizedRoot:    bellatrixBlkRoot,
			errString:        "could not notify forkchoice update from execution engine: payload status is INVALID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &mockEngineService{forkchoiceError: tt.newForkchoiceErr}
			service.cfg.ExecutionEngineCaller = engine
			_, err := service.notifyForkchoiceUpdate(ctx, tt.blk, service.headRoot(), tt.finalizedRoot)
			if tt.errString != "" {
				require.ErrorContains(t, tt.errString, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_NotifyNewPayload(t *testing.T) {
	cfg := params.BeaconConfig()
	cfg.TerminalTotalDifficulty = "2"
	params.OverrideBeaconConfig(cfg)

	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	fcs := protoarray.New(0, 0, [32]byte{'a'})
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stategen.New(beaconDB)),
		WithForkChoiceStore(fcs),
	}
	phase0State, _ := util.DeterministicGenesisState(t, 1)
	altairState, _ := util.DeterministicGenesisStateAltair(t, 1)
	bellatrixState, _ := util.DeterministicGenesisStateBellatrix(t, 2)
	blk := &ethpb.SignedBeaconBlockBellatrix{
		Block: &ethpb.BeaconBlockBellatrix{
			Body: &ethpb.BeaconBlockBodyBellatrix{
				ExecutionPayload: &v1.ExecutionPayload{},
			},
		},
	}
	a := &ethpb.SignedBeaconBlockAltair{
		Block: &ethpb.BeaconBlockAltair{
			Body: &ethpb.BeaconBlockBodyAltair{},
		},
	}
	altairBlk, err := wrapper.WrappedSignedBeaconBlock(a)
	require.NoError(t, err)
	bellatrixBlk, err := wrapper.WrappedSignedBeaconBlock(blk)
	require.NoError(t, err)
	service, err := NewService(ctx, opts...)
	require.NoError(t, err)

	tests := []struct {
		name          string
		preState      state.BeaconState
		postState     state.BeaconState
		blk           block.SignedBeaconBlock
		newPayloadErr error
		errString     string
	}{
		{
			name:      "phase 0 post state",
			postState: phase0State,
			preState:  phase0State,
		},
		{
			name:      "altair post state",
			postState: altairState,
			preState:  altairState,
		},
		{
			name:      "nil beacon block",
			postState: bellatrixState,
			preState:  bellatrixState,
			errString: "signed beacon block can't be nil",
		},
		{
			name:          "new payload with optimistic block",
			postState:     bellatrixState,
			preState:      bellatrixState,
			blk:           bellatrixBlk,
			newPayloadErr: engine.ErrAcceptedSyncingPayloadStatus,
		},
		{
			name:          "new payload with invalid block",
			postState:     bellatrixState,
			preState:      bellatrixState,
			blk:           bellatrixBlk,
			newPayloadErr: engine.ErrInvalidPayloadStatus,
			errString:     "could not validate execution payload from execution engine: payload status is INVALID",
		},
		{
			name:      "altair pre state, altair block",
			postState: bellatrixState,
			preState:  altairState,
			blk:       altairBlk,
		},
		{
			name:      "altair pre state, happy case",
			postState: bellatrixState,
			preState:  altairState,
			blk: func() block.SignedBeaconBlock {
				blk := &ethpb.SignedBeaconBlockBellatrix{
					Block: &ethpb.BeaconBlockBellatrix{
						Body: &ethpb.BeaconBlockBodyBellatrix{
							ExecutionPayload: &v1.ExecutionPayload{
								ParentHash: bytesutil.PadTo([]byte{'a'}, fieldparams.RootLength),
							},
						},
					},
				}
				b, err := wrapper.WrappedSignedBeaconBlock(blk)
				require.NoError(t, err)
				return b
			}(),
		},
		{
			name:      "could not get merge block",
			postState: bellatrixState,
			preState:  bellatrixState,
			blk:       bellatrixBlk,
			errString: "could not get merge block parent hash and total difficulty",
		},
		{
			name:      "not at merge transition",
			postState: bellatrixState,
			preState:  bellatrixState,
			blk: func() block.SignedBeaconBlock {
				blk := &ethpb.SignedBeaconBlockBellatrix{
					Block: &ethpb.BeaconBlockBellatrix{
						Body: &ethpb.BeaconBlockBodyBellatrix{
							ExecutionPayload: &v1.ExecutionPayload{
								ParentHash:    make([]byte, fieldparams.RootLength),
								FeeRecipient:  make([]byte, fieldparams.FeeRecipientLength),
								StateRoot:     make([]byte, fieldparams.RootLength),
								ReceiptsRoot:  make([]byte, fieldparams.RootLength),
								LogsBloom:     make([]byte, fieldparams.LogsBloomLength),
								PrevRandao:    make([]byte, fieldparams.RootLength),
								BaseFeePerGas: make([]byte, fieldparams.RootLength),
								BlockHash:     make([]byte, fieldparams.RootLength),
							},
						},
					},
				}
				b, err := wrapper.WrappedSignedBeaconBlock(blk)
				require.NoError(t, err)
				return b
			}(),
		},
		{
			name:      "could not get merge block",
			postState: bellatrixState,
			preState:  bellatrixState,
			blk:       bellatrixBlk,
			errString: "could not get merge block parent hash and total difficulty",
		},
		{
			name:      "happy case",
			postState: bellatrixState,
			preState:  bellatrixState,
			blk: func() block.SignedBeaconBlock {
				blk := &ethpb.SignedBeaconBlockBellatrix{
					Block: &ethpb.BeaconBlockBellatrix{
						Body: &ethpb.BeaconBlockBodyBellatrix{
							ExecutionPayload: &v1.ExecutionPayload{
								ParentHash: bytesutil.PadTo([]byte{'a'}, fieldparams.RootLength),
							},
						},
					},
				}
				b, err := wrapper.WrappedSignedBeaconBlock(blk)
				require.NoError(t, err)
				return b
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &mockEngineService{newPayloadError: tt.newPayloadErr, blks: map[[32]byte]*v1.ExecutionBlock{}}
			engine.blks[[32]byte{'a'}] = &v1.ExecutionBlock{
				ParentHash:      bytesutil.PadTo([]byte{'b'}, fieldparams.RootLength),
				TotalDifficulty: "0x2",
			}
			engine.blks[[32]byte{'b'}] = &v1.ExecutionBlock{
				ParentHash:      bytesutil.PadTo([]byte{'3'}, fieldparams.RootLength),
				TotalDifficulty: "0x1",
			}
			service.cfg.ExecutionEngineCaller = engine
			var payload *ethpb.ExecutionPayloadHeader
			if tt.preState.Version() == version.Bellatrix {
				payload, err = tt.preState.LatestExecutionPayloadHeader()
				require.NoError(t, err)
			}
			root := [32]byte{'a'}
			require.NoError(t, service.cfg.ForkChoiceStore.InsertOptimisticBlock(ctx, 0, root, root, params.BeaconConfig().ZeroHash, 0, 0))
			postVersion, postHeader, err := getStateVersionAndPayload(tt.postState)
			require.NoError(t, err)
			err = service.notifyNewPayload(ctx, tt.preState.Version(), postVersion, payload, postHeader, tt.blk, root)
			if tt.errString != "" {
				require.ErrorContains(t, tt.errString, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_NotifyNewPayload_SetOptimisticToValid(t *testing.T) {
	cfg := params.BeaconConfig()
	cfg.TerminalTotalDifficulty = "2"
	params.OverrideBeaconConfig(cfg)
	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	fcs := protoarray.New(0, 0, [32]byte{'a'})
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stategen.New(beaconDB)),
		WithForkChoiceStore(fcs),
	}
	bellatrixState, _ := util.DeterministicGenesisStateBellatrix(t, 2)
	blk := &ethpb.SignedBeaconBlockBellatrix{
		Block: &ethpb.BeaconBlockBellatrix{
			Body: &ethpb.BeaconBlockBodyBellatrix{
				ExecutionPayload: &v1.ExecutionPayload{
					ParentHash: bytesutil.PadTo([]byte{'a'}, fieldparams.RootLength),
				},
			},
		},
	}
	bellatrixBlk, err := wrapper.WrappedSignedBeaconBlock(blk)
	require.NoError(t, err)
	service, err := NewService(ctx, opts...)
	require.NoError(t, err)
	engine := &mockEngineService{blks: map[[32]byte]*v1.ExecutionBlock{}}
	engine.blks[[32]byte{'a'}] = &v1.ExecutionBlock{
		ParentHash:      bytesutil.PadTo([]byte{'b'}, fieldparams.RootLength),
		TotalDifficulty: "0x2",
	}
	engine.blks[[32]byte{'b'}] = &v1.ExecutionBlock{
		ParentHash:      bytesutil.PadTo([]byte{'3'}, fieldparams.RootLength),
		TotalDifficulty: "0x1",
	}
	service.cfg.ExecutionEngineCaller = engine
	payload, err := bellatrixState.LatestExecutionPayloadHeader()
	require.NoError(t, err)
	root := [32]byte{'c'}
	require.NoError(t, service.cfg.ForkChoiceStore.InsertOptimisticBlock(ctx, 1, root, [32]byte{'a'}, params.BeaconConfig().ZeroHash, 0, 0))
	postVersion, postHeader, err := getStateVersionAndPayload(bellatrixState)
	require.NoError(t, err)
	err = service.notifyNewPayload(ctx, bellatrixState.Version(), postVersion, payload, postHeader, bellatrixBlk, root)
	require.NoError(t, err)
	optimistic, err := service.IsOptimisticForRoot(ctx, root)
	require.NoError(t, err)
	require.Equal(t, false, optimistic)
}

func Test_IsOptimisticCandidateBlock(t *testing.T) {
	params.SetupTestConfigCleanup(t)
	params.OverrideBeaconConfig(params.MainnetConfig())

	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	fcs := protoarray.New(0, 0, [32]byte{'a'})
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stategen.New(beaconDB)),
		WithForkChoiceStore(fcs),
	}

	service, err := NewService(ctx, opts...)
	require.NoError(t, err)

	params.BeaconConfig().SafeSlotsToImportOptimistically = 128
	service.genesisTime = time.Now().Add(-time.Second * 12 * 2 * 128)

	parentBlk := util.NewBeaconBlockBellatrix()
	wrappedParentBlock, err := wrapper.WrappedSignedBeaconBlock(parentBlk)
	require.NoError(t, err)
	parentRoot, err := wrappedParentBlock.Block().HashTreeRoot()
	require.NoError(t, err)

	tests := []struct {
		name      string
		blk       block.BeaconBlock
		justified block.SignedBeaconBlock
		want      bool
	}{
		{
			name: "deep block",
			blk: func(tt *testing.T) block.BeaconBlock {
				blk := util.NewBeaconBlockBellatrix()
				blk.Block.Slot = 1
				blk.Block.ParentRoot = parentRoot[:]
				wr, err := wrapper.WrappedBellatrixBeaconBlock(blk.Block)
				require.NoError(tt, err)
				return wr
			}(t),
			justified: func(tt *testing.T) block.SignedBeaconBlock {
				blk := util.NewBeaconBlockBellatrix()
				blk.Block.Slot = 32
				blk.Block.ParentRoot = parentRoot[:]
				wr, err := wrapper.WrappedSignedBeaconBlock(blk)
				require.NoError(tt, err)
				return wr
			}(t),
			want: true,
		},
		{
			name: "shallow block, Altair justified chkpt",
			blk: func(tt *testing.T) block.BeaconBlock {
				blk := util.NewBeaconBlockAltair()
				blk.Block.Slot = 200
				blk.Block.ParentRoot = parentRoot[:]
				wr, err := wrapper.WrappedAltairBeaconBlock(blk.Block)
				require.NoError(tt, err)
				return wr
			}(t),
			justified: func(tt *testing.T) block.SignedBeaconBlock {
				blk := util.NewBeaconBlockAltair()
				blk.Block.Slot = 32
				blk.Block.ParentRoot = parentRoot[:]
				wr, err := wrapper.WrappedSignedBeaconBlock(blk)
				require.NoError(tt, err)
				return wr
			}(t),
			want: false,
		},
		{
			name: "shallow block, Bellatrix justified chkpt without execution",
			blk: func(tt *testing.T) block.BeaconBlock {
				blk := util.NewBeaconBlockBellatrix()
				blk.Block.Slot = 200
				blk.Block.ParentRoot = parentRoot[:]
				wr, err := wrapper.WrappedBellatrixBeaconBlock(blk.Block)
				require.NoError(tt, err)
				return wr
			}(t),
			justified: func(tt *testing.T) block.SignedBeaconBlock {
				blk := util.NewBeaconBlockBellatrix()
				blk.Block.Slot = 32
				blk.Block.ParentRoot = parentRoot[:]
				wr, err := wrapper.WrappedSignedBeaconBlock(blk)
				require.NoError(tt, err)
				return wr
			}(t),
			want: false,
		},
		{
			name: "shallow block, execution enabled justified chkpt",
			blk: func(tt *testing.T) block.BeaconBlock {
				blk := util.NewBeaconBlockBellatrix()
				blk.Block.Slot = 200
				blk.Block.ParentRoot = parentRoot[:]
				wr, err := wrapper.WrappedBellatrixBeaconBlock(blk.Block)
				require.NoError(tt, err)
				return wr
			}(t),
			justified: func(tt *testing.T) block.SignedBeaconBlock {
				blk := util.NewBeaconBlockBellatrix()
				blk.Block.Slot = 32
				blk.Block.ParentRoot = parentRoot[:]
				blk.Block.Body.ExecutionPayload.ParentHash = bytesutil.PadTo([]byte{'a'}, fieldparams.RootLength)
				blk.Block.Body.ExecutionPayload.FeeRecipient = bytesutil.PadTo([]byte{'a'}, fieldparams.FeeRecipientLength)
				blk.Block.Body.ExecutionPayload.StateRoot = bytesutil.PadTo([]byte{'a'}, fieldparams.RootLength)
				blk.Block.Body.ExecutionPayload.ReceiptsRoot = bytesutil.PadTo([]byte{'a'}, fieldparams.RootLength)
				blk.Block.Body.ExecutionPayload.LogsBloom = bytesutil.PadTo([]byte{'a'}, fieldparams.LogsBloomLength)
				blk.Block.Body.ExecutionPayload.PrevRandao = bytesutil.PadTo([]byte{'a'}, fieldparams.RootLength)
				blk.Block.Body.ExecutionPayload.BaseFeePerGas = bytesutil.PadTo([]byte{'a'}, fieldparams.RootLength)
				blk.Block.Body.ExecutionPayload.BlockHash = bytesutil.PadTo([]byte{'a'}, fieldparams.RootLength)
				wr, err := wrapper.WrappedSignedBeaconBlock(blk)
				require.NoError(tt, err)
				return wr
			}(t),
			want: true,
		},
	}
	for _, tt := range tests {
		jroot, err := tt.justified.Block().HashTreeRoot()
		require.NoError(t, err)
		require.NoError(t, service.cfg.BeaconDB.SaveBlock(ctx, tt.justified))
		service.store.SetJustifiedCheckpt(
			&ethpb.Checkpoint{
				Root:  jroot[:],
				Epoch: slots.ToEpoch(tt.justified.Block().Slot()),
			})
		require.NoError(t, service.cfg.BeaconDB.SaveBlock(ctx, wrappedParentBlock))

		candidate, err := service.optimisticCandidateBlock(ctx, tt.blk)
		require.NoError(t, err)
		require.Equal(t, tt.want, candidate, tt.name)
	}
}

func Test_IsOptimisticShallowExecutionParent(t *testing.T) {
	params.SetupTestConfigCleanup(t)
	params.OverrideBeaconConfig(params.MainnetConfig())

	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stategen.New(beaconDB)),
	}

	service, err := NewService(ctx, opts...)
	require.NoError(t, err)

	params.BeaconConfig().SafeSlotsToImportOptimistically = 128
	service.genesisTime = time.Now().Add(-time.Second * 12 * 2 * 128)
	payload := &v1.ExecutionPayload{
		ParentHash:    make([]byte, 32),
		FeeRecipient:  make([]byte, 20),
		StateRoot:     make([]byte, 32),
		ReceiptsRoot:  make([]byte, 32),
		LogsBloom:     make([]byte, 256),
		PrevRandao:    make([]byte, 32),
		BaseFeePerGas: bytesutil.PadTo([]byte{1, 2, 3, 4}, fieldparams.RootLength),
		BlockHash:     make([]byte, 32),
		BlockNumber:   100,
	}
	body := &ethpb.BeaconBlockBodyBellatrix{ExecutionPayload: payload}
	block := &ethpb.BeaconBlockBellatrix{Body: body, Slot: 200}
	rawSigned := &ethpb.SignedBeaconBlockBellatrix{Block: block}
	blk := util.HydrateSignedBeaconBlockBellatrix(rawSigned)
	wr, err := wrapper.WrappedSignedBeaconBlock(blk)
	require.NoError(t, err)
	require.NoError(t, service.cfg.BeaconDB.SaveBlock(ctx, wr))
	blkRoot, err := wr.Block().HashTreeRoot()
	require.NoError(t, err)

	childBlock := util.NewBeaconBlockBellatrix()
	childBlock.Block.ParentRoot = blkRoot[:]
	// shallow block
	childBlock.Block.Slot = 201
	wrappedChild, err := wrapper.WrappedSignedBeaconBlock(childBlock)
	require.NoError(t, err)
	require.NoError(t, service.cfg.BeaconDB.SaveBlock(ctx, wrappedChild))
	candidate, err := service.optimisticCandidateBlock(ctx, wrappedChild.Block())
	require.NoError(t, err)
	require.Equal(t, true, candidate)
}

func Test_GetPayloadAttribute(t *testing.T) {
	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stategen.New(beaconDB)),
		WithProposerIdsCache(cache.NewProposerPayloadIDsCache()),
	}

	// Cache miss
	service, err := NewService(ctx, opts...)
	require.NoError(t, err)
	hasPayload, _, vId, err := service.getPayloadAttribute(ctx, nil, 0)
	require.NoError(t, err)
	require.Equal(t, false, hasPayload)
	require.Equal(t, types.ValidatorIndex(0), vId)

	// Cache hit, advance state, no fee recipient
	suggestedVid := types.ValidatorIndex(1)
	slot := types.Slot(1)
	service.cfg.ProposerSlotIndexCache.SetProposerAndPayloadIDs(slot, suggestedVid, 0)
	st, _ := util.DeterministicGenesisState(t, 1)
	hook := logTest.NewGlobal()
	hasPayload, attr, vId, err := service.getPayloadAttribute(ctx, st, slot)
	require.NoError(t, err)
	require.Equal(t, true, hasPayload)
	require.Equal(t, suggestedVid, vId)
	require.Equal(t, fieldparams.EthBurnAddressHex, common.BytesToAddress(attr.SuggestedFeeRecipient).String())
	require.LogsContain(t, hook, "Fee recipient not set. Using burn address")

	// Cache hit, advance state, has fee recipient
	suggestedAddr := common.HexToAddress("123")
	require.NoError(t, service.cfg.BeaconDB.SaveFeeRecipientsByValidatorIDs(ctx, []types.ValidatorIndex{suggestedVid}, []common.Address{suggestedAddr}))
	service.cfg.ProposerSlotIndexCache.SetProposerAndPayloadIDs(slot, suggestedVid, 0)
	hasPayload, attr, vId, err = service.getPayloadAttribute(ctx, st, slot)
	require.NoError(t, err)
	require.Equal(t, true, hasPayload)
	require.Equal(t, suggestedVid, vId)
	require.Equal(t, suggestedAddr, common.BytesToAddress(attr.SuggestedFeeRecipient))
}

func Test_UpdateLastValidatedCheckpoint(t *testing.T) {
	params.SetupTestConfigCleanup(t)
	params.OverrideBeaconConfig(params.MainnetConfig())

	ctx := context.Background()
	beaconDB := testDB.SetupDB(t)
	stateGen := stategen.New(beaconDB)
	fcs := protoarray.New(0, 0, [32]byte{})
	opts := []Option{
		WithDatabase(beaconDB),
		WithStateGen(stateGen),
		WithForkChoiceStore(fcs),
	}
	service, err := NewService(ctx, opts...)
	require.NoError(t, err)

	genesisStateRoot := [32]byte{}
	genesisBlk := blocks.NewGenesisBlock(genesisStateRoot[:])
	wr, err := wrapper.WrappedSignedBeaconBlock(genesisBlk)
	require.NoError(t, err)
	assert.NoError(t, beaconDB.SaveBlock(ctx, wr))
	genesisRoot, err := genesisBlk.Block.HashTreeRoot()
	require.NoError(t, err)
	assert.NoError(t, beaconDB.SaveGenesisBlockRoot(ctx, genesisRoot))
	require.NoError(t, fcs.InsertOptimisticBlock(ctx, 0, genesisRoot, params.BeaconConfig().ZeroHash,
		params.BeaconConfig().ZeroHash, 0, 0))
	genesisSummary := &ethpb.StateSummary{
		Root: genesisStateRoot[:],
		Slot: 0,
	}
	require.NoError(t, beaconDB.SaveStateSummary(ctx, genesisSummary))

	// Get last validated checkpoint
	origCheckpoint, err := service.cfg.BeaconDB.LastValidatedCheckpoint(ctx)
	require.NoError(t, err)
	require.NoError(t, beaconDB.SaveLastValidatedCheckpoint(ctx, origCheckpoint))

	// Optimistic finalized checkpoint
	blk := util.NewBeaconBlock()
	blk.Block.Slot = 320
	blk.Block.ParentRoot = genesisRoot[:]
	wr, err = wrapper.WrappedSignedBeaconBlock(blk)
	require.NoError(t, err)
	require.NoError(t, beaconDB.SaveBlock(ctx, wr))
	opRoot, err := blk.Block.HashTreeRoot()
	require.NoError(t, err)

	opCheckpoint := &ethpb.Checkpoint{
		Root:  opRoot[:],
		Epoch: 10,
	}
	opStateSummary := &ethpb.StateSummary{
		Root: opRoot[:],
		Slot: 320,
	}
	require.NoError(t, beaconDB.SaveStateSummary(ctx, opStateSummary))
	require.NoError(t, fcs.InsertOptimisticBlock(ctx, 320, opRoot, genesisRoot,
		params.BeaconConfig().ZeroHash, 10, 10))
	assert.NoError(t, beaconDB.SaveGenesisBlockRoot(ctx, opRoot))
	require.NoError(t, service.updateFinalized(ctx, opCheckpoint))
	cp, err := service.cfg.BeaconDB.LastValidatedCheckpoint(ctx)
	require.NoError(t, err)
	require.DeepEqual(t, origCheckpoint.Root, cp.Root)
	require.Equal(t, origCheckpoint.Epoch, cp.Epoch)

	// Validated finalized checkpoint
	blk = util.NewBeaconBlock()
	blk.Block.Slot = 640
	blk.Block.ParentRoot = opRoot[:]
	wr, err = wrapper.WrappedSignedBeaconBlock(blk)
	require.NoError(t, err)
	require.NoError(t, beaconDB.SaveBlock(ctx, wr))
	validRoot, err := blk.Block.HashTreeRoot()
	require.NoError(t, err)

	validCheckpoint := &ethpb.Checkpoint{
		Root:  validRoot[:],
		Epoch: 20,
	}
	validSummary := &ethpb.StateSummary{
		Root: validRoot[:],
		Slot: 640,
	}
	require.NoError(t, beaconDB.SaveStateSummary(ctx, validSummary))
	require.NoError(t, fcs.InsertOptimisticBlock(ctx, 640, validRoot, params.BeaconConfig().ZeroHash,
		params.BeaconConfig().ZeroHash, 20, 20))
	require.NoError(t, fcs.SetOptimisticToValid(ctx, validRoot))
	assert.NoError(t, beaconDB.SaveGenesisBlockRoot(ctx, validRoot))
	require.NoError(t, service.updateFinalized(ctx, validCheckpoint))
	cp, err = service.cfg.BeaconDB.LastValidatedCheckpoint(ctx)
	require.NoError(t, err)

	optimistic, err := service.IsOptimisticForRoot(ctx, validRoot)
	require.NoError(t, err)
	require.Equal(t, false, optimistic)
	require.DeepEqual(t, validCheckpoint.Root, cp.Root)
	require.Equal(t, validCheckpoint.Epoch, cp.Epoch)
}

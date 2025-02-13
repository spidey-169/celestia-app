package qgb

import (
	"errors"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/x/qgb/keeper"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SignificantPowerDifferenceThreshold the threshold of change in the validator set power
// that would need the creation of a new valset request.
const SignificantPowerDifferenceThreshold = 0.05

// EndBlocker is called at the end of every block.
func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	// we always want to create the valset at first so that if there is a new validator set, then it is
	// the one responsible for signing from now on.
	handleValsetRequest(ctx, k)
	handleDataCommitmentRequest(ctx, k)
}

func handleDataCommitmentRequest(ctx sdk.Context, k keeper.Keeper) {
	setDataCommitmentAttestation := func() {
		dataCommitment, err := k.NextDataCommitment(ctx)
		if err != nil {
			panic(sdkerrors.Wrap(err, "couldn't get current data commitment"))
		}
		err = k.SetAttestationRequest(ctx, &dataCommitment)
		if err != nil {
			panic(err)
		}
	}
	// this will  keep executing until all the needed data commitments are created and we catchup to the current height
	for {
		hasLastDataCommitment, err := k.HasDataCommitmentInStore(ctx)
		if err != nil {
			panic(err)
		}
		if hasLastDataCommitment {
			// if the store already has a data commitment, we use it to check if we need to create a new data commitment
			lastDataCommitment, err := k.GetLastDataCommitment(ctx)
			if err != nil {
				panic(err)
			}
			if ctx.BlockHeight()-int64(lastDataCommitment.EndBlock) >= int64(k.GetDataCommitmentWindowParam(ctx)) {
				setDataCommitmentAttestation()
			} else {
				// the needed data commitments are already created and we need to wait for the next window to elapse
				break
			}
		} else {
			// if the store doesn't have a data commitment, we check if the window has passed to create a new data commitment
			if ctx.BlockHeight() >= int64(k.GetDataCommitmentWindowParam(ctx)) {
				setDataCommitmentAttestation()
			} else {
				// the first data commitment window hasn't elapsed yet to create a commitment
				break
			}
		}
	}
}

func handleValsetRequest(ctx sdk.Context, k keeper.Keeper) {
	// get the last valsets to compare against
	var latestValset *types.Valset
	if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx) != 0 {
		var err error
		latestValset, err = k.GetLatestValset(ctx)
		if err != nil {
			panic(err)
		}
	}

	lastUnbondingHeight := k.GetLastUnBondingBlockHeight(ctx)

	significantPowerDiff := false
	if latestValset != nil {
		vs, err := k.GetCurrentValset(ctx)
		if err != nil {
			// this condition should only occur in the simulator
			// ref : https://github.com/Gravity-Bridge/Gravity-Bridge/issues/35
			if errors.Is(err, types.ErrNoValidators) {
				ctx.Logger().Error("no bonded validators",
					"cause", err.Error(),
				)
				return
			}
			panic(err)
		}
		intCurrMembers, err := types.BridgeValidators(vs.Members).ToInternal()
		if err != nil {
			panic(sdkerrors.Wrap(err, "invalid current valset members"))
		}
		intLatestMembers, err := types.BridgeValidators(latestValset.Members).ToInternal()
		if err != nil {
			panic(sdkerrors.Wrap(err, "invalid latest valset members"))
		}

		significantPowerDiff = intCurrMembers.PowerDiff(*intLatestMembers) > SignificantPowerDifferenceThreshold
	}

	if (latestValset == nil) || (lastUnbondingHeight == uint64(ctx.BlockHeight())) || significantPowerDiff {
		// if the conditions are true, put in a new validator set request to be signed and submitted to EVM
		valset, err := k.GetCurrentValset(ctx)
		if err != nil {
			panic(err)
		}
		err = k.SetAttestationRequest(ctx, &valset)
		if err != nil {
			panic(err)
		}
	}
}

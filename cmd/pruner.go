package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"cosmossdk.io/log"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	//capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"

	packetforwardtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	icqtypes "github.com/cosmos/ibc-apps/modules/async-icq/v8/types"
	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibchost "github.com/cosmos/ibc-go/v8/modules/core/exported"

	"cosmossdk.io/store/iavl"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/types"
	storetypes "cosmossdk.io/store/types"
	evidencetypes "cosmossdk.io/x/evidence/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	db "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/state"
	tmstore "github.com/cometbft/cometbft/store"
	dbm "github.com/cosmos/cosmos-db"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	crisistypes "github.com/cosmos/cosmos-sdk/x/crisis/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/neilotoole/errgroup"
	"github.com/spf13/cobra"
	"github.com/syndtr/goleveldb/leveldb/opt"

	"cosmossdk.io/store/rootmulti"
	// "github.com/osmosis-labs/cosmprund/internal/rootmulti"
)

// load db
// load app store and prune
// if immutable tree is not deletable we should import and export current state

func pruneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune [path_to_home]",
		Short: "prune data from the application store and block store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			ctx := cmd.Context()
			errs, _ := errgroup.WithContext(ctx)
			var err error
			if tendermint {
				errs.Go(func() error {
					if err = pruneTMData(args[0]); err != nil {
						return err
					}
					return nil
				})
			}

			if cosmosSdk {
				err = pruneAppState(args[0])
				if err != nil {
					return err
				}
				return nil

			}

			return errs.Wait()
		},
	}
	return cmd
}

func pruneAppState(home string) error {

	// this has the potential to expand size, should just use state sync
	// dbType := db.BackendType(backend)

	dbDir := rootify(dataDir, home)

	o := opt.Options{
		DisableSeeksCompaction: true,
	}

	// Get BlockStore
	appDB, err := dbm.NewGoLevelDBWithOpts("application", dbDir, &o)
	if err != nil {
		return err
	}

	//TODO: need to get all versions in the store, setting randomly is too slow
	fmt.Println("pruning application state")

	// only mount keys from core sdk
	// todo allow for other keys to be mounted
	keys := storetypes.NewKVStoreKeys(
		authtypes.StoreKey, banktypes.StoreKey, authzkeeper.StoreKey, stakingtypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey, ibchost.StoreKey,
		icahosttypes.StoreKey,
		icqtypes.StoreKey,
		evidencetypes.StoreKey, minttypes.StoreKey, govtypes.StoreKey, ibctransfertypes.StoreKey,
		packetforwardtypes.StoreKey,
		paramstypes.StoreKey, consensusparamtypes.StoreKey, crisistypes.StoreKey, upgradetypes.StoreKey,
		// feegrant.StoreKey,
	)

	if app == "osmosis" {
		osmoKeys := storetypes.NewKVStoreKeys(
			"downtimedetector",
			"hooks-for-ibc",
			"lockup", //lockuptypes.StoreKey,
			"concentratedliquidity",
			"gamm", // gammtypes.StoreKey,
			"cosmwasmpool",
			"poolmanager",
			"twap",
			"epochs", // epochstypes.StoreKey,
			"protorev",
			"txfees",         // txfeestypes.StoreKey,
			"incentives",     // incentivestypes.StoreKey,
			"poolincentives", //poolincentivestypes.StoreKey,
			"tokenfactory",   //tokenfactorytypes.StoreKey,
			"valsetpref",
			"superfluid",   // superfluidtypes.StoreKey,
			"wasm",         // wasm.StoreKey,
			"smartaccount", // smartaccount.StoreKey,
			//"rate-limited-ibc", // there is no store registered for this module
		)
		for key, value := range osmoKeys {
			keys[key] = value
		}
	}

	// TODO: cleanup app state
	logger := log.NewLogger(os.Stderr)

	appStore := rootmulti.NewStore(appDB, logger, metrics.NewMetrics([][]string{}))

	for _, value := range keys {
		appStore.MountStoreWithDB(value, storetypes.StoreTypeIAVL, nil)
		appStore.SetIAVLDisableFastNode(true)
	}

	err = appStore.LoadLatestVersion()
	if err != nil {
		return err
	}

	latestHeight := rootmulti.GetLatestVersion(appDB)
	// valid heights should be greater than 0.
	if latestHeight <= 0 {
		return fmt.Errorf("the database has no valid heights to prune, the latest height: %v", latestHeight)
	}

	if err = PruneStores(appStore, latestHeight); err != nil {
		return err
	}
	fmt.Println("pruning application state complete")

	fmt.Println("compacting application state")
	if err := appDB.ForceCompact(nil, nil); err != nil {
		return err
	}
	fmt.Println("compacting application state complete")

	//create a new app store
	return nil
}

func PruneStores(rs *rootmulti.Store, pruningHeight int64) (err error) {
	if pruningHeight <= 0 {
		fmt.Println("pruning skipped, height is less than or equal to 0")
		return nil
	}

	fmt.Println("pruning all stores", "heights", pruningHeight)
	storeKeysByName := rs.StoreKeysByName()

	// Iterate over the map
	for storeName, storeKey := range storeKeysByName {
		// Get the store using the store key
		store := rs.GetCommitKVStore(storeKey)

		// If the store is wrapped with an inter-block cache, we must first unwrap
		// it to get the underlying IAVL store.
		if store.GetStoreType() != types.StoreTypeIAVL {
			continue
		}

		versions := store.(*iavl.Store).GetAllVersions()
		versionExists := store.(*iavl.Store).VersionExists(int64(versions[0]))
		fmt.Printf("Store %s: %d versions (latest: %d, exists: %v)\n", storeName, len(versions), versions[0], versionExists)

		// Start sync pruning because of custom iavl version
		// go get github.com/osmosis-labs/iavl@08fd812d460bcc95a2c733fdbaa11b53ec16b424
		err := store.(*iavl.Store).DeleteVersionsTo(pruningHeight - 2)
		if err != nil {
			return err
		}
	}

	return nil
}

// pruneTMData prunes the tendermint blocks and state based on the amount of blocks to keep
func pruneTMData(home string) error {

	dbDir := rootify(dataDir, home)

	o := opt.Options{
		DisableSeeksCompaction: true,
	}

	// Get BlockStore
	blockStoreDB, err := db.NewGoLevelDBWithOpts("blockstore", dbDir, &o)
	if err != nil {
		return err
	}
	blockStore := tmstore.NewBlockStore(blockStoreDB)

	// Get StateStore
	stateDB, err := db.NewGoLevelDBWithOpts("state", dbDir, &o)
	if err != nil {
		return err
	}

	stateStore := state.NewStore(stateDB, state.StoreOptions{
		DiscardABCIResponses: true,
	})
	stateData, err := stateStore.LoadFromDBOrGenesisFile("")
	if err != nil {
		return err
	}

	base := blockStore.Base()

	pruneHeight := blockStore.Height() - int64(blocks)

	errs, _ := errgroup.WithContext(context.Background())
	errs.Go(func() error {
		fmt.Println("pruning block store")
		// prune block store
		blocks, _, err = blockStore.PruneBlocks(pruneHeight, stateData)
		if err != nil {
			return err
		}
		fmt.Println("pruning block store complete")

		fmt.Println("compacting block store")
		if err := blockStoreDB.Compact(nil, nil); err != nil {
			return err
		}

		fmt.Println("compacting block store complete")

		return nil
	})

	fmt.Println("pruning state store")
	// prune state store
	// evidenceThreshold is the height at which evidence is deleted
	// we need to keep at least 2 weeks of evidence which is roughly 1000000 blocks
	evidenceThreshold := int64(1000000)
	// Prune states will prune from base or pruneHeight-evidenceThreshold whichever is lower
	err = stateStore.PruneStates(base, pruneHeight, pruneHeight-evidenceThreshold)
	if err != nil {
		return err
	}
	fmt.Println("pruning state store complete")

	fmt.Println("compacting state store")
	if err := stateDB.Compact(nil, nil); err != nil {
		return err
	}
	fmt.Println("compacting state store complete")

	return nil
}

// Utils

func rootify(path, root string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

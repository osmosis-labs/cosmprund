package cmd

import (
	"fmt"
	"os"

	"cosmossdk.io/log"
	"cosmossdk.io/store/iavl"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	"cosmossdk.io/store/types"
	storetypes "cosmossdk.io/store/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

func pruneStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune-store [path_to_home] [store_name]",
		Short: "prune data from a specific application store",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			home := args[0]
			storeName := args[1]
			return pruneSpecificStore(home, storeName)
		},
	}
	return cmd
}

func pruneSpecificStore(home, storeName string) error {
	dbDir := rootify(dataDir, home)

	o := opt.Options{
		DisableSeeksCompaction: true,
	}

	// Get BlockStore
	appDB, err := dbm.NewGoLevelDBWithOpts("application", dbDir, &o)
	if err != nil {
		return err
	}

	fmt.Printf("pruning store: %s\n", storeName)

	logger := log.NewLogger(os.Stderr)
	appStore := rootmulti.NewStore(appDB, logger, metrics.NewMetrics([][]string{}))

	// Create an empty map of keys
	keys := storetypes.NewKVStoreKeys()

	osmoKeys := storetypes.NewKVStoreKeys(
		storeName,
	)
	for key, value := range osmoKeys {
		keys[key] = value
	}

	for _, value := range keys {
		appStore.MountStoreWithDB(value, storetypes.StoreTypeIAVL, nil)
		appStore.SetIAVLDisableFastNode(true)
	}

	err = appStore.LoadLatestVersion()
	if err != nil {
		return err
	}

	latestHeight := rootmulti.GetLatestVersion(appDB)
	if latestHeight <= 0 {
		return fmt.Errorf("the database has no valid heights to prune, the latest height: %v", latestHeight)
	}

	storeKey, exists := keys[storeName]
	if !exists {
		return fmt.Errorf("store %s does not exist", storeName)
	}

	store := appStore.GetCommitKVStore(storeKey)
	if store == nil {
		return fmt.Errorf("store %s not found", storeName)
	}

	if store.GetStoreType() != types.StoreTypeIAVL {
		return fmt.Errorf("store %s is not an IAVL store", storeName)
	}

	iavlStore := store.(*iavl.Store)
	versions := iavlStore.GetAllVersions()
	versionExists := iavlStore.VersionExists(int64(versions[0]))
	fmt.Printf("Store %s: %d versions (latest: %d, exists: %v)\n", storeName, len(versions), versions[0], versionExists)

	// Start sync pruning because of custom iavl version
	// go get github.com/osmosis-labs/iavl@08fd812d460bcc95a2c733fdbaa11b53ec16b424
	err = iavlStore.DeleteVersionsTo(latestHeight - 2)
	if err != nil {
		return err
	}

	fmt.Printf("pruning store %s complete\n", storeName)

	fmt.Println("compacting application state")
	//if err := appDB.ForceCompact(nil, nil); err != nil {
	//	return err
	//}
	fmt.Println("compacting application state complete")

	return nil
}

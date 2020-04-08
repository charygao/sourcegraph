package server

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestCleanFailedUploads(t *testing.T) {
	withRoot(t, func(bundleDir string) {
		mtimes := map[string]time.Time{
			"u1": time.Now().Local().Add(-time.Minute * 3),  // older than 1m
			"u2": time.Now().Local().Add(-time.Minute * 2),  // older than 1m
			"u3": time.Now().Local().Add(-time.Second * 30), // newer than 1m
			"u4": time.Now().Local().Add(-time.Second * 20), // newer than 1m
		}

		for name, mtime := range mtimes {
			path := filepath.Join(bundleDir, "uploads", name)
			if err := makeFile(path, mtime); err != nil {
				t.Fatalf("unexpected error creating file %s: %s", path, err)
			}
		}

		j := &Janitor{
			bundleDir:               bundleDir,
			maxUnconvertedUploadAge: time.Minute,
		}

		if err := j.cleanFailedUploads(); err != nil {
			t.Fatalf("unexpected error cleaning failed uploads: %s", err)
		}

		names, err := getFilenames(filepath.Join(bundleDir, "uploads"))
		if err != nil {
			t.Fatalf("unexpected error listing directory: %s", err)
		}

		expected := []string{"u3", "u4"}
		if !reflect.DeepEqual(names, expected) {
			t.Errorf("unexpected directory contents: want=%s have=%s", expected, names)
		}
	})
}

func TestRemoveDeadDumps(t *testing.T) {
	withRoot(t, func(bundleDir string) {
		ids := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

		for _, id := range ids {
			path := filepath.Join(bundleDir, "dbs", fmt.Sprintf("%d.lsif.db", id))
			if err := makeFile(path, time.Now().Local()); err != nil {
				t.Fatalf("unexpected error creating file %s: %s", path, err)
			}
		}

		j := &Janitor{
			bundleDir: bundleDir,
		}

		var idArgs [][]int
		statesFn := func(ctx context.Context, args []int) (map[int]string, error) {
			sort.Ints(args)
			idArgs = append(idArgs, args)

			return map[int]string{
				1:  "completed",
				2:  "queued",
				3:  "completed",
				4:  "processing",
				5:  "completed",
				9:  "errored",
				10: "errored",
			}, nil
		}

		if err := j.removeDeadDumps(statesFn); err != nil {
			t.Fatalf("unexpected error removing dead dumps: %s", err)
		}

		names, err := getFilenames(filepath.Join(bundleDir, "dbs"))
		if err != nil {
			t.Fatalf("unexpected error listing directory: %s", err)
		}

		if expected := []string{"1.lsif.db", "2.lsif.db", "3.lsif.db", "4.lsif.db", "5.lsif.db"}; !reflect.DeepEqual(names, expected) {
			t.Errorf("unexpected directory contents: want=%v have=%v", expected, names)
		}

		if expected := [][]int{ids}; !reflect.DeepEqual(idArgs, expected) {
			t.Errorf("unexpected arguments to statesFn: want=%v have=%v", expected, idArgs)
		}
	})
}

func TestRemoveDeadDumpsMaxRequestBatchSize(t *testing.T) {
	withRoot(t, func(bundleDir string) {
		var ids []int
		for i := 1; i <= 225; i++ {
			ids = append(ids, i)
		}

		for _, id := range ids {
			path := filepath.Join(bundleDir, "dbs", fmt.Sprintf("%d.lsif.db", id))
			if err := makeFile(path, time.Now().Local()); err != nil {
				t.Fatalf("unexpected error creating file %s: %s", path, err)
			}
		}

		j := &Janitor{
			bundleDir: bundleDir,
		}

		var idArgs [][]int
		statesFn := func(ctx context.Context, args []int) (map[int]string, error) {
			idArgs = append(idArgs, args)

			states := map[int]string{}
			for _, arg := range args {
				if arg%2 == 0 {
					states[arg] = "completed"
				}
			}
			return states, nil
		}

		if err := j.removeDeadDumps(statesFn); err != nil {
			t.Fatalf("unexpected error removing dead dumps: %s", err)
		}

		names, err := getFilenames(filepath.Join(bundleDir, "dbs"))
		if err != nil {
			t.Fatalf("unexpected error listing directory: %s", err)
		}

		if len(names) != 112 {
			t.Errorf("unexpected directory file count: want=%v have=%v", 112, len(names))
		}

		var allArgs []int
		for _, args := range idArgs {
			if len(args) > DeadDumpBatchSize {
				t.Errorf("unexpected large slice: want < %d have=%d", DeadDumpBatchSize, len(args))
			}

			allArgs = append(allArgs, args...)
		}
		sort.Ints(allArgs)

		if !reflect.DeepEqual(allArgs, ids) {
			t.Errorf("unexpected flattened arguments to statesFn: want=%v have=%v", ids, allArgs)
		}
	})
}

func TestCleanOldDumpsStopsAfterFreeingDesiredSpace(t *testing.T) {
	withRoot(t, func(bundleDir string) {
		sizes := map[int]int{
			1:  20,
			2:  20,
			3:  20,
			4:  20,
			5:  20,
			6:  20,
			7:  20,
			8:  20,
			9:  20,
			10: 20,
		}

		for id, size := range sizes {
			path := filepath.Join(bundleDir, "dbs", fmt.Sprintf("%d.lsif.db", id))
			if err := makeFileWithSize(path, size); err != nil {
				t.Fatalf("unexpected error creating file %s: %s", path, err)
			}
		}

		calls := 0
		pruneFn := func(ctx context.Context) (int64, bool, error) {
			calls++
			return int64(calls), true, nil
		}

		j := &Janitor{
			bundleDir: bundleDir,
		}

		if err := j.cleanOldDumps(pruneFn, 100); err != nil {
			t.Fatalf("unexpected error cleaning old dumps: %s", err)
		}

		names, err := getFilenames(filepath.Join(bundleDir, "dbs"))
		if err != nil {
			t.Fatalf("unexpected error listing directory: %s", err)
		}

		if expected := []string{"10.lsif.db", "6.lsif.db", "7.lsif.db", "8.lsif.db", "9.lsif.db"}; !reflect.DeepEqual(names, expected) {
			t.Errorf("unexpected directory contents: want=%v have=%v", expected, names)
		}
	})
}

func TestCleanOldDumpsStopsWithNoPrunableDatabases(t *testing.T) {
	withRoot(t, func(bundleDir string) {
		sizes := map[int]int{
			1:  10,
			2:  10,
			3:  10,
			4:  10,
			5:  10,
			6:  10,
			7:  10,
			8:  10,
			9:  10,
			10: 10,
		}

		for id, size := range sizes {
			path := filepath.Join(bundleDir, "dbs", fmt.Sprintf("%d.lsif.db", id))
			if err := makeFileWithSize(path, size); err != nil {
				t.Fatalf("unexpected error creating file %s: %s", path, err)
			}
		}

		idsToPrune := []int{1, 2, 3, 4, 5}
		pruneFn := func(ctx context.Context) (int64, bool, error) {
			if len(idsToPrune) == 0 {
				return 0, false, nil
			}

			id := idsToPrune[0]
			idsToPrune = idsToPrune[1:]
			return int64(id), true, nil
		}

		j := &Janitor{
			bundleDir: bundleDir,
		}

		if err := j.cleanOldDumps(pruneFn, 100); err != nil {
			t.Fatalf("unexpected error cleaning old dumps: %s", err)
		}

		names, err := getFilenames(filepath.Join(bundleDir, "dbs"))
		if err != nil {
			t.Fatalf("unexpected error listing directory: %s", err)
		}

		if expected := []string{"10.lsif.db", "6.lsif.db", "7.lsif.db", "8.lsif.db", "9.lsif.db"}; !reflect.DeepEqual(names, expected) {
			t.Errorf("unexpected directory contents: want=%v have=%v", expected, names)
		}
	})
}

func TestBatchIntSlice(t *testing.T) {
	batches := batchIntSlice([]int{1, 2, 3, 4, 5, 6, 7, 8, 9}, 2)
	expected := [][]int{
		[]int{1, 2},
		[]int{3, 4},
		[]int{5, 6},
		[]int{7, 8},
		[]int{9},
	}

	if !reflect.DeepEqual(batches, expected) {
		t.Errorf("unexpected batch layout: want=%v have=%v", expected, batches)
	}
}

func withRoot(t *testing.T, testFunc func(bundleDir string)) {
	bundleDir, err := ioutil.TempDir("", "precise-code-intel-bundle-manager-")
	if err != nil {
		t.Fatalf("unexpected error creating test directory: %s", err)
	}
	defer os.RemoveAll(bundleDir)

	for _, dir := range []string{"", "uploads", "dbs"} {
		path := filepath.Join(bundleDir, dir)
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			t.Fatalf("unexpected error creating test directory: %s", err)
		}
	}

	testFunc(bundleDir)
}

func makeFile(path string, mtimes time.Time) error {
	if file, err := os.Create(path); err != nil {
		return err
	} else {
		file.Close()
	}

	return os.Chtimes(path, mtimes, mtimes)
}

func makeFileWithSize(path string, size int) error {
	return ioutil.WriteFile(path, make([]byte, size), 0644)
}

func getFilenames(path string) ([]string, error) {
	infos, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, info := range infos {
		names = append(names, info.Name())
	}
	sort.Strings(names)

	return names, nil
}
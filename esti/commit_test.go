package esti

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/treeverse/lakefs/pkg/api/apigen"
)

func TestCommitSingle(t *testing.T) {
	ctx, _, repo := setupTest(t)
	defer tearDownTest(repo)

	const objPath = "1.txt"
	_, objContent := uploadFileRandomData(ctx, t, repo, mainBranch, objPath)
	commitResp, err := client.CommitWithResponse(ctx, repo, mainBranch, &apigen.CommitParams{}, apigen.CommitJSONRequestBody{
		Message: "singleCommit",
	})
	require.NoError(t, err, "failed to commit changes")
	require.NoErrorf(t, verifyResponse(commitResp.HTTPResponse, commitResp.Body),
		"failed to commit changes repo %s branch %s", repo, mainBranch)

	getObjResp, err := client.GetObjectWithResponse(ctx, repo, mainBranch, &apigen.GetObjectParams{Path: objPath})
	require.NoError(t, err, "failed to get object")
	require.NoErrorf(t, verifyResponse(getObjResp.HTTPResponse, getObjResp.Body),
		"failed to get object repo %s branch %s path %s", repo, mainBranch, objPath)

	body := string(getObjResp.Body)
	require.Equalf(t, objContent, body, "path: %s, expected: %s, actual:%s", objPath, objContent, body)
}

// genNames generates n consecutive filenames starting with prefix.
func genNames(n int, prefix string) []string {
	ret := make([]string, n)
	for i := range ret {
		ret[i] = fmt.Sprint(prefix, i+1)
	}
	return ret
}

type Upload struct {
	Repo, Branch, Path string
}

// upload uploads random file data for uploads.
func upload(ctx context.Context, uploads chan Upload) error {
	for u := range uploads {
		_, _, err := uploadFileRandomDataAndReport(ctx, u.Repo, u.Branch, u.Path, false)
		if err != nil {
			return err
		}
	}
	return nil
}

func TestCommitInMixedOrder(t *testing.T) {
	const (
		parallelism = 5
		size        = 100
	)

	ctx, _, repo := setupTest(t)
	defer tearDownTest(repo)
	names1 := genNames(size, "run2/foo")
	uploads := make(chan Upload, size)
	wg := sync.WaitGroup{}
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			if err := upload(ctx, uploads); err != nil {
				t.Error(err)
			}
			wg.Done()
		}()
	}
	for _, name := range names1 {
		uploads <- Upload{Repo: repo, Branch: mainBranch, Path: name}
	}
	close(uploads)
	wg.Wait()

	if t.Failed() {
		t.FailNow()
	}

	commitResp, err := client.CommitWithResponse(ctx, repo, mainBranch, &apigen.CommitParams{}, apigen.CommitJSONRequestBody{
		Message: "mixedOrderCommit1",
	})
	require.NoError(t, err, "failed to commit changes")
	require.NoErrorf(t, verifyResponse(commitResp.HTTPResponse, commitResp.Body),
		"failed to commit changes repo %s branch %s", repo, mainBranch)

	names2 := genNames(size, "run1/foo")
	uploads = make(chan Upload, size)
	wg = sync.WaitGroup{}
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			if err := upload(ctx, uploads); err != nil {
				t.Error(err)
			}
			wg.Done()
		}()
	}
	for _, name := range names2 {
		uploads <- Upload{Repo: repo, Branch: mainBranch, Path: name}
	}
	close(uploads)
	wg.Wait()

	if t.Failed() {
		t.FailNow()
	}

	commitResp, err = client.CommitWithResponse(ctx, repo, mainBranch, &apigen.CommitParams{}, apigen.CommitJSONRequestBody{
		Message: "mixedOrderCommit2",
	})
	require.NoError(t, err, "failed to commit second set of changes")
	require.NoErrorf(t, verifyResponse(commitResp.HTTPResponse, commitResp.Body),
		"failed to commit second set of changes repo %s branch %s", repo, mainBranch)
}

// Verify panic fix when committing with nil tombstone over KV
func TestCommitWithTombstone(t *testing.T) {
	ctx, _, repo := setupTest(t)
	defer tearDownTest(repo)
	origObjPathLow := "objb.txt"
	origObjPathHigh := "objc.txt"
	uploadFileRandomData(ctx, t, repo, mainBranch, origObjPathLow)
	uploadFileRandomData(ctx, t, repo, mainBranch, origObjPathHigh)
	commitResp, err := client.CommitWithResponse(ctx, repo, mainBranch, &apigen.CommitParams{}, apigen.CommitJSONRequestBody{
		Message: "First commit",
	})
	require.NoError(t, err, "failed to commit changes")
	require.NoErrorf(t, verifyResponse(commitResp.HTTPResponse, commitResp.Body),
		"failed to commit changes repo %s branch %s", repo, mainBranch)

	tombstoneObjPath := "obja.txt"
	newObjPath := "objd.txt"
	uploadFileRandomData(ctx, t, repo, mainBranch, tombstoneObjPath)
	uploadFileRandomData(ctx, t, repo, mainBranch, newObjPath)

	// Turning tombstoneObjPath to tombstone
	resp, err := client.DeleteObjectWithResponse(ctx, repo, mainBranch, &apigen.DeleteObjectParams{Path: tombstoneObjPath})
	require.NoError(t, err, "failed to delete object")
	require.Equal(t, http.StatusNoContent, resp.StatusCode())

	commitResp, err = client.CommitWithResponse(ctx, repo, mainBranch, &apigen.CommitParams{}, apigen.CommitJSONRequestBody{
		Message: "Commit with tombstone",
	})
	require.NoError(t, err, "failed to commit changes")
	require.NoErrorf(t, verifyResponse(commitResp.HTTPResponse, commitResp.Body),
		"failed to commit changes repo %s branch %s", repo, mainBranch)
}

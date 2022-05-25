package helper

import (
	"testing"

	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

func TestCache(t *testing.T) {
	cache := NewWorkResourceCache()

	requiredObj := spoketesting.NewUnstructured("foo.test.io/v1", "TestKind", "testns", "test")
	existingObj := spoketesting.NewUnstructured("foo.test.io/v1", "TestKind", "testns", "test")
	existingObj.SetResourceVersion("test1")
	existingObj.SetGeneration(1)

	// do not skip if cache is empty
	if cache.SafeToSkipApply(requiredObj, existingObj) {
		t.Errorf("should not skip since cache is empty")
	}

	if cache.SafeToSkipApplyWithGeneration(requiredObj, existingObj) {
		t.Errorf("should not skip since cache is empty")
	}

	// skip if object is in the cache
	cache.UpdateCachedResourceMetadata(requiredObj, existingObj)

	if !cache.SafeToSkipApply(requiredObj, existingObj) {
		t.Errorf("should skip since object is added in the cache")
	}

	if !cache.SafeToSkipApplyWithGeneration(requiredObj, existingObj) {
		t.Errorf("should skip since object is added in the cache")
	}

	// do not skip if manifest is changed
	requiredObj2 := spoketesting.NewUnstructured("v1", "TestKind", "testns", "test1")
	if cache.SafeToSkipApply(requiredObj2, existingObj) {
		t.Errorf("should not skip since resource manifest is changed")
	}

	if cache.SafeToSkipApplyWithGeneration(requiredObj2, existingObj) {
		t.Errorf("should not skip since resource manifest is changed")
	}

	// do not skip if resource version is changed
	existingObj.SetResourceVersion("test2")

	if cache.SafeToSkipApply(requiredObj, existingObj) {
		t.Errorf("should not skip since resource version is changed")
	}

	if !cache.SafeToSkipApplyWithGeneration(requiredObj, existingObj) {
		t.Errorf("should skip since generation stays")
	}

	// do not skip if generation is changed
	existingObj.SetResourceVersion("test1")
	existingObj.SetGeneration(2)

	if !cache.SafeToSkipApply(requiredObj, existingObj) {
		t.Errorf("should skip since resource version stays")
	}

	if cache.SafeToSkipApplyWithGeneration(requiredObj, existingObj) {
		t.Errorf("should not skip since generation changes")
	}

	// Remove cache
	cache.RemoveCache(existingObj)

	if cache.SafeToSkipApply(requiredObj, existingObj) {
		t.Errorf("should not skip since cache is empty")
	}

	if cache.SafeToSkipApplyWithGeneration(requiredObj, existingObj) {
		t.Errorf("should not skip since cache is empty")
	}
}

// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0
//
// Credits: The Flux Authors
//     https://github.com/fluxcd/kustomize-controller/blob/main/internal/inventory/inventory.go

package inventory

import (
	"fmt"
	"sort"

	"github.com/fluxcd/pkg/ssa"
	mpasv1alpha1 "github.com/open-component-model/mpas-project-controller/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/object"
)

func New() *mpasv1alpha1.ResourceInventory {
	return &mpasv1alpha1.ResourceInventory{
		Entries: []mpasv1alpha1.ResourceRef{},
	}
}

// Add adds a new object reference to the inventory.
func Add(inv *mpasv1alpha1.ResourceInventory, objs ...runtime.Object) error {
	for _, obj := range objs {
		objMetadata, err := object.RuntimeToObjMeta(obj)
		if err != nil {
			return fmt.Errorf("could not get object metadata: %w", err)
		}

		entry := mpasv1alpha1.ResourceRef{
			ID:      objMetadata.String(),
			Version: obj.GetObjectKind().GroupVersionKind().GroupVersion().String(),
		}
		inv.Entries = append(inv.Entries, entry)
	}

	return nil
}

// List return the inventory entries as unstructured.Unstructured objects.
func List(inv *mpasv1alpha1.ResourceInventory) ([]*unstructured.Unstructured, error) {
	objects := make([]*unstructured.Unstructured, 0)

	if inv.Entries == nil {
		return objects, nil
	}

	for _, entry := range inv.Entries {
		objMetadata, err := object.ParseObjMetadata(entry.ID)
		if err != nil {
			return nil, fmt.Errorf("could not parse object metadata: %w", err)
		}

		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   objMetadata.GroupKind.Group,
			Kind:    objMetadata.GroupKind.Kind,
			Version: entry.Version,
		})
		u.SetName(objMetadata.Name)
		u.SetNamespace(objMetadata.Namespace)
		objects = append(objects, u)
	}

	sort.Sort(ssa.SortableUnstructureds(objects))

	return objects, nil
}

// ListMetadata returns the inventory entries as object.ObjMetadata objects.
func ListMetadata(inv *mpasv1alpha1.ResourceInventory) (object.ObjMetadataSet, error) {
	var metas []object.ObjMetadata
	for _, e := range inv.Entries {
		m, err := object.ParseObjMetadata(e.ID)
		if err != nil {
			return metas, err
		}
		metas = append(metas, m)
	}

	return metas, nil
}

// Diff returns the slice of objects that do not exist in the target inventory.
func Diff(source *mpasv1alpha1.ResourceInventory, target *mpasv1alpha1.ResourceInventory) ([]*unstructured.Unstructured, error) {
	getVersion := func(inv *mpasv1alpha1.ResourceInventory, objMetadata object.ObjMetadata) string {
		for _, entry := range inv.Entries {
			if entry.ID == objMetadata.String() {
				return entry.Version
			}
		}
		return ""
	}

	objects := make([]*unstructured.Unstructured, 0)
	aList, err := ListMetadata(source)
	if err != nil {
		return nil, err
	}

	bList, err := ListMetadata(target)
	if err != nil {
		return nil, err
	}

	list := aList.Diff(bList)
	if len(list) == 0 {
		return objects, nil
	}

	for _, metadata := range list {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   metadata.GroupKind.Group,
			Kind:    metadata.GroupKind.Kind,
			Version: getVersion(source, metadata),
		})
		u.SetName(metadata.Name)
		u.SetNamespace(metadata.Namespace)
		objects = append(objects, u)
	}

	sort.Sort(ssa.SortableUnstructureds(objects))
	return objects, nil
}

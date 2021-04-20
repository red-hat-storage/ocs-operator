package util

import (
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ComposePredicates will compose a variable number of predicates and return a predicate that
// will allow events that are allowed by any of the given predicates.
func ComposePredicates(predicates ...predicate.Predicate) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			for _, p := range predicates {
				if p != nil && p.Create(e) {
					return true
				}
			}
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			for _, p := range predicates {
				if p != nil && p.Delete(e) {
					return true
				}
			}
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			for _, p := range predicates {
				if p != nil && p.Update(e) {
					return true
				}
			}
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			for _, p := range predicates {
				if p != nil && p.Generic(e) {
					return true
				}
			}
			return false
		},
	}
}

// MetadataChangedPredicate will only allow events that changed labels,
// annotations, or finalizers
type MetadataChangedPredicate struct {
	predicate.Funcs
}

// Update implements the update event trap for StorageClusterChangedPredicate
func (p MetadataChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil {
		return false
	}

	if e.ObjectNew == nil {
		return false
	}

	metaChanged := !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) ||
		!reflect.DeepEqual(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations()) ||
		!reflect.DeepEqual(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers())

	return metaChanged
}

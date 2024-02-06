package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var now = metav1.NewTime(time.Now())

func TestComposePredicatesUpdate(t *testing.T) {
	cases := []struct {
		label   string
		metaNew *metav1.ObjectMeta
		update  bool
	}{
		{
			label:   "no update",
			metaNew: &metav1.ObjectMeta{},
			update:  false,
		},
		{
			label: "labels update",
			metaNew: &metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			update: true,
		},
		{
			label: "resourceversion update",
			metaNew: &metav1.ObjectMeta{
				ResourceVersion: "foo",
			},
			update: true,
		},
		{
			label: "any other field",
			metaNew: &metav1.ObjectMeta{
				Name:                       "foo",
				GenerateName:               "foo",
				Namespace:                  "foo",
				SelfLink:                   "foo",
				UID:                        types.UID("foo"),
				Generation:                 1,
				CreationTimestamp:          now,
				DeletionTimestamp:          &now,
				DeletionGracePeriodSeconds: ptr.To(int64(1)),
				OwnerReferences:            []metav1.OwnerReference{{}},
				ManagedFields:              []metav1.ManagedFieldsEntry{{}},
			},
			update: false,
		},
	}

	for i, c := range cases {
		t.Logf("Case %d: %s\n", i+1, c.label)
		pred := ComposePredicates(
			predicate.ResourceVersionChangedPredicate{},
			MetadataChangedPredicate{},
		)
		event := event.UpdateEvent{
			ObjectOld: &corev1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{},
			},
			ObjectNew: &corev1.PersistentVolume{
				ObjectMeta: *c.metaNew,
			},
		}

		assert.Equal(t, c.update, pred.Update(event))
	}
}

func TestMetadataChangedPredicateUpdate(t *testing.T) {
	cases := []struct {
		label   string
		metaNew *metav1.ObjectMeta
		update  bool
	}{
		{
			label:   "no update",
			metaNew: &metav1.ObjectMeta{},
			update:  false,
		},
		{
			label: "labels update",
			metaNew: &metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			update: true,
		},
		{
			label: "annotations update",
			metaNew: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"foo": "bar",
				},
			},
			update: true,
		},
		{
			label: "finalizers update",
			metaNew: &metav1.ObjectMeta{
				Finalizers: []string{
					"foo",
				},
			},
			update: true,
		},
		{
			label: "any other field",
			metaNew: &metav1.ObjectMeta{
				Name:                       "foo",
				GenerateName:               "foo",
				Namespace:                  "foo",
				SelfLink:                   "foo",
				UID:                        types.UID("foo"),
				ResourceVersion:            "foo",
				Generation:                 1,
				CreationTimestamp:          now,
				DeletionTimestamp:          &now,
				DeletionGracePeriodSeconds: ptr.To(int64(1)),
				OwnerReferences:            []metav1.OwnerReference{{}},
				ManagedFields:              []metav1.ManagedFieldsEntry{{}},
			},
			update: false,
		},
	}

	for i, c := range cases {
		t.Logf("Case %d: %s\n", i+1, c.label)
		pred := MetadataChangedPredicate{}
		event := event.UpdateEvent{
			ObjectOld: &corev1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{},
			},
			ObjectNew: &corev1.PersistentVolume{
				ObjectMeta: *c.metaNew,
			},
		}

		assert.Equal(t, c.update, pred.Update(event))
	}
}

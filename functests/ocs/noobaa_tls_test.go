package ocs_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	tests "github.com/red-hat-storage/ocs-operator/v4/functests"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NooBaaTLS struct {
	client      client.Client
	namespace   string
	tlsProfile  *ocstlsv1.TLSProfile
	noobaa      *nbv1.NooBaa
	origNooBaa  *nbv1.NooBaa
	origProfile *ocstlsv1.TLSProfile
}

func initNooBaaTLS() (*NooBaaTLS, error) {
	retObj := &NooBaaTLS{}
	retObj.client = tests.DeployManager.Client
	retObj.namespace = tests.InstallNamespace
	return retObj, nil
}

func (obj *NooBaaTLS) getNooBaa() error {
	obj.noobaa = &nbv1.NooBaa{}
	err := obj.client.Get(context.TODO(), client.ObjectKey{
		Name:      "noobaa",
		Namespace: obj.namespace,
	}, obj.noobaa)
	if err != nil {
		return err
	}
	obj.origNooBaa = obj.noobaa.DeepCopy()
	return nil
}

func (obj *NooBaaTLS) getTLSProfile() error {
	obj.tlsProfile = &ocstlsv1.TLSProfile{}
	err := obj.client.Get(context.TODO(), client.ObjectKey{
		Name:      defaults.TLSProfileName,
		Namespace: obj.namespace,
	}, obj.tlsProfile)
	if err != nil {
		if errors.IsNotFound(err) {
			// TLSProfile doesn't exist, which is valid
			obj.tlsProfile = nil
			return nil
		}
		return err
	}
	obj.origProfile = obj.tlsProfile.DeepCopy()
	return nil
}

func (obj *NooBaaTLS) createTLSProfile(version ocstlsv1.TLSProtocolVersion, ciphers []ocstlsv1.TLSCipherSuite, groups []ocstlsv1.TLSGroupName) error {
	obj.tlsProfile = &ocstlsv1.TLSProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.TLSProfileName,
			Namespace: obj.namespace,
		},
		Spec: ocstlsv1.TLSProfileSpec{
			Rules: []ocstlsv1.TLSProfileRules{
				{
					Selectors: []ocstlsv1.Selector{"noobaa.io"},
					Config: ocstlsv1.TLSConfig{
						Version: version,
						Ciphers: ciphers,
						Groups:  groups,
					},
				},
			},
		},
	}
	return obj.client.Create(context.TODO(), obj.tlsProfile)
}

func (obj *NooBaaTLS) updateTLSProfile(version ocstlsv1.TLSProtocolVersion, ciphers []ocstlsv1.TLSCipherSuite, groups []ocstlsv1.TLSGroupName) error {
	if obj.tlsProfile == nil {
		return fmt.Errorf("TLSProfile not loaded")
	}
	obj.tlsProfile.Spec.Rules = []ocstlsv1.TLSProfileRules{
		{
			Selectors: []ocstlsv1.Selector{"noobaa.io"},
			Config: ocstlsv1.TLSConfig{
				Version: version,
				Ciphers: ciphers,
				Groups:  groups,
			},
		},
	}
	return obj.client.Update(context.TODO(), obj.tlsProfile)
}

func (obj *NooBaaTLS) deleteTLSProfile() error {
	if obj.tlsProfile == nil {
		return nil
	}
	err := obj.client.Delete(context.TODO(), obj.tlsProfile)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	obj.tlsProfile = nil
	return nil
}

func (obj *NooBaaTLS) restoreOriginalState() error {
	if obj.origProfile != nil {
		currentProfile := &ocstlsv1.TLSProfile{}
		err := obj.client.Get(context.TODO(), client.ObjectKeyFromObject(obj.origProfile), currentProfile)
		if err != nil {
			if errors.IsNotFound(err) {
				return obj.client.Create(context.TODO(), obj.origProfile)
			}
			return err
		}
		currentProfile.Spec = obj.origProfile.Spec
		return obj.client.Update(context.TODO(), currentProfile)
	}
	return obj.deleteTLSProfile()
}

func (obj *NooBaaTLS) waitForNooBaaReconcile(expectedVersion *nbv1.TLSProtocolVersion, expectedCiphers []string) {
	gomega.Eventually(func() error {
		nb := &nbv1.NooBaa{}
		err := obj.client.Get(context.TODO(), client.ObjectKey{
			Name:      "noobaa",
			Namespace: obj.namespace,
		}, nb)
		if err != nil {
			return err
		}

		if nb.Spec.Security.APIServerSecurity == nil {
			if expectedVersion == nil {
				return nil
			}
			return fmt.Errorf("NooBaa APIServerSecurity is nil, expected TLS configuration")
		}

		if expectedVersion == nil {
			return fmt.Errorf("NooBaa has TLS configuration but none was expected")
		}

		if nb.Spec.Security.APIServerSecurity.TLSMinVersion == nil {
			return fmt.Errorf("TLSMinVersion is nil")
		}
		if *nb.Spec.Security.APIServerSecurity.TLSMinVersion != *expectedVersion {
			return fmt.Errorf("TLSMinVersion mismatch: got %v, expected %v",
				*nb.Spec.Security.APIServerSecurity.TLSMinVersion, *expectedVersion)
		}

		if len(nb.Spec.Security.APIServerSecurity.TLSCiphers) != len(expectedCiphers) {
			return fmt.Errorf("TLSCiphers count mismatch: got %d, expected %d",
				len(nb.Spec.Security.APIServerSecurity.TLSCiphers), len(expectedCiphers))
		}

		cipherMap := make(map[string]bool)
		for _, cipher := range nb.Spec.Security.APIServerSecurity.TLSCiphers {
			cipherMap[cipher] = true
		}
		for _, expectedCipher := range expectedCiphers {
			if !cipherMap[expectedCipher] {
				return fmt.Errorf("expected cipher %s not found in NooBaa TLSCiphers", expectedCipher)
			}
		}

		return nil
	}, 2*time.Minute, 5*time.Second).ShouldNot(gomega.HaveOccurred())
}

var _ = ginkgo.Describe("NooBaa TLS Configuration", NooBaaTLSTest)

func NooBaaTLSTest() {
	var tlsObj *NooBaaTLS
	var err error

	ginkgo.BeforeEach(func() {
		tlsObj, err = initNooBaaTLS()
		gomega.Expect(err).To(gomega.BeNil())

		err = tlsObj.getNooBaa()
		gomega.Expect(err).To(gomega.BeNil())

		err = tlsObj.getTLSProfile()
		gomega.Expect(err).To(gomega.BeNil())
	})

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			tests.SuiteFailed = tests.SuiteFailed || true
		}

		if tlsObj != nil {
			err := tlsObj.restoreOriginalState()
			gomega.Expect(err).To(gomega.BeNil())
		}
	})

	ginkgo.Describe("TLS Profile to NooBaa CR Transfer", func() {
		ginkgo.Context("when TLSProfile is created with TLS 1.2", func() {
			ginkgo.It("should configure NooBaa with TLS 1.2 settings", func() {
				ginkgo.By("Creating TLSProfile with TLS 1.2")
				_ = tlsObj.deleteTLSProfile()

				err = tlsObj.createTLSProfile(
					ocstlsv1.VersionTLS1_2,
					[]ocstlsv1.TLSCipherSuite{
						"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
						"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
					},
					[]ocstlsv1.TLSGroupName{"secp256r1", "secp384r1"},
				)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying NooBaa CR is updated with TLS 1.2 configuration")
				tlsObj.waitForNooBaaReconcile(
					ptr.To(nbv1.VersionTLS12),
					[]string{
						tls.CipherSuiteName(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256),
						tls.CipherSuiteName(tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384),
					},
				)
			})
		})

		ginkgo.Context("when TLSProfile is created with TLS 1.3", func() {
			ginkgo.It("should configure NooBaa with TLS 1.3 settings", func() {
				ginkgo.By("Creating TLSProfile with TLS 1.3")
				_ = tlsObj.deleteTLSProfile()

				err = tlsObj.createTLSProfile(
					ocstlsv1.VersionTLS1_3,
					[]ocstlsv1.TLSCipherSuite{
						"TLS_AES_128_GCM_SHA256",
						"TLS_AES_256_GCM_SHA384",
					},
					[]ocstlsv1.TLSGroupName{"secp256r1", "X25519"},
				)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying NooBaa CR is updated with TLS 1.3 configuration")
				tlsObj.waitForNooBaaReconcile(
					ptr.To(nbv1.VersionTLS13),
					[]string{
						tls.CipherSuiteName(tls.TLS_AES_128_GCM_SHA256),
						tls.CipherSuiteName(tls.TLS_AES_256_GCM_SHA384),
					},
				)
			})
		})

		ginkgo.Context("when TLSProfile is updated", func() {
			ginkgo.It("should update NooBaa CR with new TLS settings", func() {
				ginkgo.By("Creating initial TLSProfile with TLS 1.2")
				_ = tlsObj.deleteTLSProfile()

				err = tlsObj.createTLSProfile(
					ocstlsv1.VersionTLS1_2,
					[]ocstlsv1.TLSCipherSuite{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
					[]ocstlsv1.TLSGroupName{"secp256r1"},
				)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Waiting for initial configuration")
				tlsObj.waitForNooBaaReconcile(
					ptr.To(nbv1.VersionTLS12),
					[]string{tls.CipherSuiteName(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)},
				)

				ginkgo.By("Updating TLSProfile to TLS 1.3 with curves")
				err = tlsObj.updateTLSProfile(
					ocstlsv1.VersionTLS1_3,
					[]ocstlsv1.TLSCipherSuite{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384"},
					[]ocstlsv1.TLSGroupName{"secp256r1", "secp384r1", "X25519"},
				)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying NooBaa CR is updated with TLS 1.3 configuration")
				tlsObj.waitForNooBaaReconcile(
					ptr.To(nbv1.VersionTLS13),
					[]string{
						tls.CipherSuiteName(tls.TLS_AES_128_GCM_SHA256),
						tls.CipherSuiteName(tls.TLS_AES_256_GCM_SHA384),
					},
				)

				ginkgo.By("Verifying TLS Groups are configured")
				nb := &nbv1.NooBaa{}
				err = tlsObj.client.Get(context.TODO(), client.ObjectKey{
					Name:      "noobaa",
					Namespace: tlsObj.namespace,
				}, nb)
				gomega.Expect(err).To(gomega.BeNil())
				gomega.Expect(nb.Spec.Security.APIServerSecurity.TLSGroups).To(gomega.ConsistOf(
					nbv1.TLSGroupSecp256r1,
					nbv1.TLSGroupSecp384r1,
					nbv1.TLSGroupX25519,
				))
			})
		})

		ginkgo.Context("when TLSProfile is deleted", func() {
			ginkgo.It("should remove TLS configuration from NooBaa CR", func() {
				ginkgo.By("Creating TLSProfile")
				_ = tlsObj.deleteTLSProfile()

				err = tlsObj.createTLSProfile(
					ocstlsv1.VersionTLS1_2,
					[]ocstlsv1.TLSCipherSuite{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
					[]ocstlsv1.TLSGroupName{"secp256r1"},
				)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Waiting for TLS configuration to propagate")
				tlsObj.waitForNooBaaReconcile(
					ptr.To(nbv1.VersionTLS12),
					[]string{tls.CipherSuiteName(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)},
				)

				ginkgo.By("Deleting TLSProfile")
				err = tlsObj.deleteTLSProfile()
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying NooBaa CR has no TLS configuration")
				tlsObj.waitForNooBaaReconcile(nil, nil)
			})
		})

		ginkgo.Context("when TLSProfile has multiple rules", func() {
			ginkgo.It("should only apply the noobaa.io matching rule", func() {
				ginkgo.By("Creating TLSProfile with rules for multiple selectors")
				_ = tlsObj.deleteTLSProfile()

				tlsObj.tlsProfile = &ocstlsv1.TLSProfile{
					ObjectMeta: metav1.ObjectMeta{
						Name:      defaults.TLSProfileName,
						Namespace: tlsObj.namespace,
					},
					Spec: ocstlsv1.TLSProfileSpec{
						Rules: []ocstlsv1.TLSProfileRules{
							{
								// Rule for other.service - should NOT be applied to NooBaa
								Selectors: []ocstlsv1.Selector{"other.service"},
								Config: ocstlsv1.TLSConfig{
									Version: ocstlsv1.VersionTLS1_2,
									Ciphers: []ocstlsv1.TLSCipherSuite{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
									Groups:  []ocstlsv1.TLSGroupName{"secp256r1"},
								},
							},
							{
								// Rule for noobaa.io - should be applied to NooBaa
								Selectors: []ocstlsv1.Selector{"noobaa.io"},
								Config: ocstlsv1.TLSConfig{
									Version: ocstlsv1.VersionTLS1_3,
									Ciphers: []ocstlsv1.TLSCipherSuite{"TLS_AES_256_GCM_SHA384"},
									Groups:  []ocstlsv1.TLSGroupName{"X25519"},
								},
							},
						},
					},
				}
				err = tlsObj.client.Create(context.TODO(), tlsObj.tlsProfile)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying NooBaa CR uses the noobaa.io rule (TLS 1.3)")
				tlsObj.waitForNooBaaReconcile(
					ptr.To(nbv1.VersionTLS13),
					[]string{tls.CipherSuiteName(tls.TLS_AES_256_GCM_SHA384)},
				)
			})
		})
	})

	ginkgo.Describe("TLS Group Configuration", func() {
		ginkgo.Context("when TLSProfile specifies elliptic curve groups", func() {
			ginkgo.It("should configure NooBaa with the corresponding TLS groups", func() {
				ginkgo.By("Creating TLSProfile with P-256, P-384, and X25519 groups")
				_ = tlsObj.deleteTLSProfile()

				err = tlsObj.createTLSProfile(
					ocstlsv1.VersionTLS1_3,
					[]ocstlsv1.TLSCipherSuite{"TLS_AES_128_GCM_SHA256"},
					[]ocstlsv1.TLSGroupName{"secp256r1", "secp384r1", "X25519"},
				)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying NooBaa CR has the expected TLS groups")
				gomega.Eventually(func() error {
					nb := &nbv1.NooBaa{}
					err := tlsObj.client.Get(context.TODO(), client.ObjectKey{
						Name:      "noobaa",
						Namespace: tlsObj.namespace,
					}, nb)
					if err != nil {
						return err
					}
					if nb.Spec.Security.APIServerSecurity == nil {
						return fmt.Errorf("APIServerSecurity is nil")
					}
					if len(nb.Spec.Security.APIServerSecurity.TLSGroups) == 0 {
						return fmt.Errorf("TLSGroups is empty")
					}
					expectedGroups := map[nbv1.TLSGroup]bool{
						nbv1.TLSGroupSecp256r1: true,
						nbv1.TLSGroupSecp384r1: true,
						nbv1.TLSGroupX25519:    true,
					}
					for _, group := range nb.Spec.Security.APIServerSecurity.TLSGroups {
						if !expectedGroups[group] {
							return fmt.Errorf("unexpected TLS group: %v", group)
						}
						delete(expectedGroups, group)
					}
					if len(expectedGroups) > 0 {
						return fmt.Errorf("missing expected TLS groups: %v", expectedGroups)
					}
					return nil
				}, 2*time.Minute, 5*time.Second).ShouldNot(gomega.HaveOccurred())
			})
		})

		ginkgo.Context("when TLSProfile specifies hybrid post-quantum groups", func() {
			ginkgo.It("should configure NooBaa with X25519MLKEM768 group", func() {
				ginkgo.By("Creating TLSProfile with X25519MLKEM768 group (TLS 1.3 only)")
				_ = tlsObj.deleteTLSProfile()

				err = tlsObj.createTLSProfile(
					ocstlsv1.VersionTLS1_3,
					[]ocstlsv1.TLSCipherSuite{"TLS_AES_128_GCM_SHA256"},
					[]ocstlsv1.TLSGroupName{"X25519MLKEM768"},
				)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying NooBaa CR has X25519MLKEM768 TLS group")
				gomega.Eventually(func() error {
					nb := &nbv1.NooBaa{}
					err := tlsObj.client.Get(context.TODO(), client.ObjectKey{
						Name:      "noobaa",
						Namespace: tlsObj.namespace,
					}, nb)
					if err != nil {
						return err
					}
					if nb.Spec.Security.APIServerSecurity == nil {
						return fmt.Errorf("APIServerSecurity is nil")
					}
					for _, group := range nb.Spec.Security.APIServerSecurity.TLSGroups {
						if group == nbv1.TLSGroupX25519MLKEM768 {
							return nil
						}
					}
					return fmt.Errorf("X25519MLKEM768 group not found in TLSGroups: %v",
						nb.Spec.Security.APIServerSecurity.TLSGroups)
				}, 2*time.Minute, 5*time.Second).ShouldNot(gomega.HaveOccurred())
			})
		})
	})
}

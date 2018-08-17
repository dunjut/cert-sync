package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	core_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsTlsSecret(t *testing.T) {
	assert := assert.New(t)
	positives := []*core_v1.Secret{
		&core_v1.Secret{
			Type: core_v1.SecretTypeTLS,
		},
	}
	negatives := []*core_v1.Secret{
		&core_v1.Secret{
			Type: core_v1.SecretTypeOpaque,
		},
		&core_v1.Secret{
			Type: core_v1.SecretTypeSSHAuth,
		},
		&core_v1.Secret{
			Type: core_v1.SecretTypeDockercfg,
		},
		&core_v1.Secret{
			Type: core_v1.SecretTypeBasicAuth,
		},
		&core_v1.Secret{
			Type: core_v1.SecretTypeDockerConfigJson,
		},
		&core_v1.Secret{
			Type: core_v1.SecretTypeServiceAccountToken,
		},
	}
	for _, s := range positives {
		assert.True(isTlsSecret(s))
	}
	for _, s := range negatives {
		assert.False(isTlsSecret(s))
	}
}

func TestWantAutoSync(t *testing.T) {
	assert := assert.New(t)
	positives := []*core_v1.Secret{
		&core_v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CertSyncAnnotationKey: "true",
				},
			},
		},
	}
	negatives := []*core_v1.Secret{
		&core_v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		},
		&core_v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CertSyncAnnotationKey: "false",
				},
			},
		},
		&core_v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CertSyncAnnotationKey: "somethingwrong",
				},
			},
		},
	}
	for _, s := range positives {
		assert.True(wantAutoSync(s))
	}
	for _, s := range negatives {
		assert.False(wantAutoSync(s))
	}
}

func TestIsTlsSecretWithAutoSync(t *testing.T) {
	assert := assert.New(t)
	positives := []*core_v1.Secret{
		&core_v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CertSyncAnnotationKey: "true",
				},
			},
			Type: core_v1.SecretTypeTLS,
		},
	}
	negatives := []*core_v1.Secret{
		&core_v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CertSyncAnnotationKey: "false",
				},
			},
			Type: core_v1.SecretTypeTLS,
		},
		&core_v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CertSyncAnnotationKey: "true",
				},
			},
			Type: core_v1.SecretTypeOpaque,
		},
		&core_v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CertSyncAnnotationKey: "false",
				},
			},
			Type: core_v1.SecretTypeOpaque,
		},
	}
	for _, s := range positives {
		assert.True(isTlsSecretWithAutoSync(s))
	}
	for _, s := range negatives {
		assert.False(isTlsSecretWithAutoSync(s))
	}
}

func TestFullSecretName(t *testing.T) {
	var (
		assert = assert.New(t)
		secret = &core_v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-tls-secret",
				Namespace: "test-namespace",
			},
		}
	)
	assert.Equal("test-namespace/test-tls-secret", fullSecretName(secret))
}

type secretpair struct {
	s1 *core_v1.Secret
	s2 *core_v1.Secret
}

func makeSecret() *core_v1.Secret {
	return &core_v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				CertSyncAnnotationKey: "true",
			},
			Labels: map[string]string{},
		},
		Data: map[string][]byte{
			core_v1.TLSPrivateKeyKey: []byte("privatekeydata"),
			core_v1.TLSCertKey:       []byte("certificatedata"),
		},
		Type: core_v1.SecretTypeTLS,
	}

}

func makeSecretPair() *secretpair {
	return &secretpair{
		s1: makeSecret(),
		s2: makeSecret(),
	}
}

func (sp *secretpair) changeAnnotation() *secretpair {
	delete(sp.s2.Annotations, CertSyncAnnotationKey)
	return sp
}

func (sp *secretpair) changePrivateKeyData() *secretpair {
	sp.s2.Data[core_v1.TLSPrivateKeyKey] = []byte("new-privatekeydata")
	return sp
}

func (sp *secretpair) changeCertificateData() *secretpair {
	sp.s2.Data[core_v1.TLSCertKey] = []byte("new-certificatedata")
	return sp
}

func (sp *secretpair) changeNoneRelatedConfigurations() *secretpair {
	sp.s2.Annotations["someNoneRelatedKey"] = "someNoneRelatedValue"
	sp.s2.Labels["someNoneRelatedLabelKey"] = "someNoneRelatedLabelValue"
	return sp
}

func TestHaveConcernedUpdate(t *testing.T) {
	assert := assert.New(t)
	positives := []*secretpair{
		makeSecretPair().changeAnnotation(),
		makeSecretPair().changePrivateKeyData(),
		makeSecretPair().changeCertificateData(),
		makeSecretPair().changeAnnotation().changePrivateKeyData().changeCertificateData(),
	}
	negatives := []*secretpair{
		makeSecretPair(),
		makeSecretPair().changeNoneRelatedConfigurations(),
	}
	for _, sp := range positives {
		assert.True(haveConcernedUpdate(sp.s1, sp.s2))
	}
	for _, sp := range negatives {
		assert.False(haveConcernedUpdate(sp.s1, sp.s2))
	}
}

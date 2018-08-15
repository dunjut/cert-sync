package agent

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/golang/glog"
	core_v1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
)

const (
	CertSyncAnnotationKey = "certsync.istio.io/autosync"
)

// Run will start the controller.
// stopCh channel is used to send interrupt signal to stop it.
func (a *Agent) Run(stopCh <-chan struct{}) {
	// don't let panics crash the process
	defer utilruntime.HandleCrash()
	// make sure the workqueue is shutdown which will trigger workers to end
	defer a.queue.ShutDown()

	glog.Info("starting cert-sync agent...")

	go a.controller.Run(stopCh)

	// wait for the cache to synchronize before starting workers
	if !cache.WaitForCacheSync(stopCh, a.controller.HasSynced) {
		glog.Error("timed out waiting for cache to sync")
		return
	}

	// TODO
	for i := 0; i < 1; i++ {
		go wait.Until(a.runWorker, time.Second, stopCh)
	}
	<-stopCh
	glog.Info("stopping cert-sync agent...")
}

func (a *Agent) runWorker() {
	for a.processNextItem() {
	}
}

func (a *Agent) processNextItem() bool {
	// wait until there is new item in the working queue
	item, quit := a.queue.Get()
	if quit {
		return false
	}
	// tell the queue that we are done with processing this item. This unblocks the item for other workers.
	// This allows safe parallel processing because two secrets with the same key are never processed in
	// parallel.
	defer a.queue.Done(item)

	secret := item.(*core_v1.Secret)
	if err := a.ensureCertificateSynchronized(secret); err != nil {
		// no retry mechanism right now, just log error and keep going
		glog.Errorf("synchronize %s failed: %v", fullSecretName(secret), err)
	} else {
		glog.Infof("synchronize %s succeeded.", fullSecretName(secret))
	}
	a.queue.Forget(item)
	return true
}

func (a *Agent) ensureCertificateSynchronized(secret *core_v1.Secret) error {
	// we'll place TLS certificate and TLS private key into sub-directory of
	// user specified directory (--certDir), as
	//   $certPath/$namespace/$name.key
	//   $certPath/$namespace/$name.crt
	//
	// e.g.
	//   /etc/istio/ingress-certs/kube-system/foo.key
	//   /etc/istio/ingress-certs/kube-system/foo.crt
	var (
		subdir          = path.Join(a.certDir, secret.Namespace)
		privateKeyPath  = path.Join(subdir, fmt.Sprintf("%s.key", secret.Name))
		certificatePath = path.Join(subdir, fmt.Sprintf("%s.crt", secret.Name))
	)
	if err := ensureCertificateDir(subdir); err != nil {
		return err
	}

	// ensure private key file exists and contains correct data
	if err := ensureFileData(privateKeyPath, secret.Data[core_v1.TLSPrivateKeyKey]); err != nil {
		return err
	}
	// ensure certificate file exists and contains correct data
	if err := ensureFileData(certificatePath, secret.Data[core_v1.TLSCertKey]); err != nil {
		return err
	}

	return nil
}

func (a *Agent) ensureCertificateDeleted(secret *core_v1.Secret) error {
	var (
		subdir          = path.Join(a.certDir, secret.Namespace)
		privateKeyPath  = path.Join(subdir, fmt.Sprintf("%s.key", secret.Name))
		certificatePath = path.Join(subdir, fmt.Sprintf("%s.crt", secret.Name))
	)

	if err := os.Remove(privateKeyPath); err != nil {
		return err
	}

	if err := os.Remove(certificatePath); err != nil {
		return err
	}

	// TODO(dunjut) remove subdir if no more certificates exists?

	return nil
}

func ensureFileData(filename string, data []byte) error {
	if _, err := os.Stat(filename); err != nil {
		if os.IsNotExist(err) {
			return ioutil.WriteFile(filename, data, 0666)
		}
		return err
	}

	oldData, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	if bytes.Equal(oldData, data) {
		return nil
	} else {
		return ioutil.WriteFile(filename, data, 0666)
	}
}

func ensureCertificateDir(dir string) error {
	if err := os.Mkdir(dir, 0777); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func (a *Agent) secretsEventHandlerAdd(obj interface{}) {
	secret := obj.(*core_v1.Secret)
	// if it's not a TLS Secret or user doesn't tell us to automatically
	// synchronize it, then we don't care about it's change.
	if !isTlsSecretWithAutoSync(secret) {
		return
	}
	a.queue.Add(obj)
}

func (a *Agent) secretsEventHandlerUpdate(oldObj, newObj interface{}) {
	oldSecret := oldObj.(*core_v1.Secret)
	newSecret := newObj.(*core_v1.Secret)
	// if the change between old and new Secrets doesn't belong to our concern,
	// for example both of them do not want autosync, or nothing changed in tls
	// data, then just return and do nothing.
	if !haveConcernedUpdate(oldSecret, newSecret) {
		return
	}

	a.queue.Add(newObj)
}

func (a *Agent) secretsEventHandlerDelete(obj interface{}) {
	secret := obj.(*core_v1.Secret)
	// if it's not a TLS Secret or user doesn't tell us to automatically
	// synchronize it, then we don't care about it's change.
	if !isTlsSecretWithAutoSync(secret) {
		return
	}

	if err := a.ensureCertificateDeleted(secret); err != nil {
		glog.Warningf("remove %s failed: %v", fullSecretName(secret), err)
	} else {
		glog.Infof("remove %s succeeded.", fullSecretName(secret))
	}
}

// check if secret is TLS type and expects auto-sync
func isTlsSecretWithAutoSync(s *core_v1.Secret) bool {
	return isTlsSecret(s) && wantAutoSync(s)
}

// check if secret is TLS type
func isTlsSecret(s *core_v1.Secret) bool {
	return s.Type == core_v1.SecretTypeTLS
}

// check if user wants to automatically synchronize this secret
func wantAutoSync(s *core_v1.Secret) bool {
	return s.Annotations[CertSyncAnnotationKey] == "true"
}

// haveConcernedUpdate checks if there's any update we concerned, include
//   - change of certsync.istio.io/autosync annotation
//   - change of TLS key and/or cert
func haveConcernedUpdate(oldSecret, newSecret *core_v1.Secret) bool {
	// fast path 1:
	//   neither old nor new Secret want autosync,
	//   don't care about its change then.
	if !wantAutoSync(oldSecret) && !wantAutoSync(newSecret) {
		return false
	}
	// fast path 2:
	//   values of old and new Secret for autosync are different,
	//   we definitely have to handle this case then.
	if wantAutoSync(oldSecret) != wantAutoSync(newSecret) {
		return true
	}

	// slow path:
	//   compare their TLS keys and certs.
	if !bytes.Equal(oldSecret.Data[core_v1.TLSPrivateKeyKey], newSecret.Data[core_v1.TLSPrivateKeyKey]) {
		return true
	}
	if !bytes.Equal(oldSecret.Data[core_v1.TLSCertKey], newSecret.Data[core_v1.TLSCertKey]) {
		return true
	}

	// something trivial in their specs changed,
	// but we don't care
	return false
}

func fullSecretName(s *core_v1.Secret) string {
	return fmt.Sprintf("%s/%s", s.Namespace, s.Name)
}

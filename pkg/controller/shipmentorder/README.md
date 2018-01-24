## Setting up development environment

Assuming Minikube is already installed and running.

Install Helm and Chart Museum:

* `brew install hg glide kubernetes-helm`
* `helm repo add incubator https://kubernetes-charts-incubator.storage.googleapis.com`
* `helm install incubator/chartmuseum`

Change the Museum service so it can be discovered by Minikube:

* `kubectl delete svc zinc-snail-chartmuseum`
* `kubectl expose deployment zinc-snail-chartmuseum --type=NodePort`

These should work now:

* `minikube service list`
* `curl http://$(minikube service --url zinc-snail-chartmuseum)/index.yaml`
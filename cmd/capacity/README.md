To run:

Set up minikube: `minikube start`.

Then, create the CRD and example ShipmentOrder:

`kubectl create -f crd/ShipmentOrder-crd.yaml`

`kubectl create -f crd/example/shipmentorder.yaml`

Now inspect the ShipmentOrder you just made:

`kubectl get -o yaml so/ship-it`

Notice that there is no `status` field.

Next, run the controller with your Minikube Kubeconfig as argument:

`go run cmd/shipmentorder/main.go -kubeconfig ~/.kube/config`

Now inspect the shipment order again: 

`kubectl get -o yaml so/ship-it`

And notice that `status` is "touched by the controller".

Magic!

# kubectl create -f ~/Documents/github-issues/event-loss/pod-nginx.yaml
# kubectl create -f ~/Documents/github-issues/event-loss/constraint-pod.yaml
# kubectl create -f ~/Documents/github-issues/event-loss/twocontainer.yaml
# kubectl get pods
# kubectl create -f https://raw.githubusercontent.com/mhausenblas/kbe/master/specs/labels/pod.yaml
# kubectl label pods labelex owner=michael
# kubectl get pods --selector owner=michael
# kubectl create -f https://raw.githubusercontent.com/mhausenblas/kbe/master/specs/labels/anotherpod.yaml
# kubectl get pods -l 'env in (production, development)'
# kubectl create -f https://raw.githubusercontent.com/mhausenblas/kbe/master/specs/rcs/rc.yaml
# kubectl get rc
# kubectl scale --replicas=3 rc/rcex
# kubectl delete rc rcex
# kubectl create -f https://raw.githubusercontent.com/mhausenblas/kbe/master/specs/deployments/d09.yaml
# kubectl get deploy
# kubectl get rs
# kubectl get pods
# kubectl apply -f https://raw.githubusercontent.com/mhausenblas/kbe/master/specs/deployments/d10.yaml
# kubectl rollout history deploy/sise-deploy
# kubectl rollout undo deploy/sise-deploy --to-revision=1

kubectl create -f ~/Documents/github-issues/event-loss/pod-nginx.yaml
sleep 2
kubectl delete pod nginx
sleep 2
kubectl create -f ~/Documents/github-issues/event-loss/pod-nginx.yaml
sleep 2
kubectl delete pod nginx
sleep 2
kubectl create -f ~/Documents/github-issues/event-loss/twocontainer.yaml
sleep 2
kubectl delete pod twocontainer
sleep 2
kubectl create -f ~/Documents/github-issues/event-loss/twocontainer.yaml
sleep 2
kubectl delete pod twocontainer
sleep 2
kubectl create -f ~/Documents/github-issues/event-loss/pod-nginx.yaml
sleep 2
kubectl create -f ~/Documents/github-issues/event-loss/twocontainer.yaml
sleep 2
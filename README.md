# jellyfin-metrics
jelyfin metrics server for more than dotnet crap



# For making the .env file
```sh
kubectl create secret generic env-secret \
  --from-file=.env=./.env \
  --namespace <deployment namespace>
```

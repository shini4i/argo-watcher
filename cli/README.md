# cli
This is just an example of what can be used within pipeline to communicate with argo-watcher
## Single image monitoring
```bash
./cli.py --app example-app --author test --image example --tag v1.8.0
```
## Multiple images monitoring:
```bash
./cli.py --app example-app --author test --image example --image example2 --tag v1.8.0
```

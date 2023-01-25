## Manual deployment of Artemis Operator
Resource files specified in this directory are necessary for manual deployment of Artemis Operator.
You have an option to deploy Artemis Operator managing only resources in its own deployed namespace, or you can make it
manage all resources across multiple namespaces if you have cluster wide admin access.
For single namespace usage you should move files `060_cluster_role` and `070_cluster_role_binding` out of `install` directory or you can see deployment permission issues.
For cluster-wide Operator management of artemis components you would need to update `WATCH_NAMESPACE` value.
We numbered files for cases, when they need to be manually applied one by one.

## Operator watching single namespace

Deploy whole `install` folder, except 2 files. Move out of that folder `060_cluster_role` and `070_cluster_role_binding` files.

```shell
mv deploy/install/*_cluster_role*yaml .
kubectl create -f deploy/install # -n <namespace>
```

Alternatively you can use script to deploy it for you
```shell
./deploy/install_opr.sh
```

## Operator watching all namespaces

Change value of **WATCH_NAMESPACE** environment variable in `110_operator.yaml` file to `"*"` or empty string (see example).
You should be fine with deploying whole `install` folder as is, for cluster wide Artemis Operator deployment.

```yaml
        - name: WATCH_NAMESPACE
          value: "*"
```

```shell
kubectl create -f deploy/install # -n <namespace>
```

And change the subjects `<namespace>` to match your target namespace in `070_cluster_role_binding.yaml` file using command:
```shell
sed -i 's/namespace: .*/namespace: <namespace>/' install/070_cluster_role_binding.yaml
```

Alternatively you can use script to deploy it for you
```shell
./deploy/cluster_wide_install_opr.sh
```

## Undeploy Operator
 
To undeploy deployed Operator using these deploy yaml files, just execute following command:
```shell
kubectl delete -f deploy/install # -n <namespace>
```

Or use simple script
```shell
./deploy/undeploy_all.sh
```

### What are these yaml files in deploy folder

These yaml files serve for manual deployment of Artemis Operator. 
They are generated from the *generate-deploy* make target located in 
[ActiveMQ Artemis Cloud](https://github.com/artemiscloud/activemq-artemis-operator) project.

#### Note ####

If you make any changes to the CRD definitions or any other config resources, you need to regenerate these YAML files 
by run the following command from the project root.

```
make generate-deploy
```

# User Guide

For a quick introduction on how to build and install the Kubernetes Operator for Apache Spark, and how to run some example applications, please refer to the [Quick Start Guide](quick-start-guide.md). For a complete reference of the API definition of the `SparkApplication` and `ScheduledSparkApplication` custom resources, please refer to the [API Specification](api.md). 

The Kubernetes Operator for Apache Spark ships with a command-line tool called `sparkctl` that offers additional features beyond what `kubectl` is able to do. Documentation on `sparkctl` can be found in [README](../sparkctl/README.md). If you are running the Spark Operator on Google Kubernetes Engine and want to use Google Cloud Storage (GCS) and/or BigQuery for reading/writing data, also refer to the [GCP guide](gcp.md). The Kubernetes Operator for Apache Spark will simply be referred to as the operator for the rest of this guide.  

## Table of Contents

* [Using a SparkApplication](#using-a-sparkapplication)
* [Writing a SparkApplication Spec](#writing-a-sparkapplication-spec)
    * [Specifying Application Dependencies](#specifying-application-dependencies)
    * [Specifying Spark Configuration](#specifying-spark-configuration)
    * [Specifying Hadoop Configuration](#specifying-hadoop-configuration)
    * [Writing Driver Specification](#writing-driver-specification)
    * [Writing Executor Specification](#writing-executor-specification)
    * [Specifying Extra Java Options](#specifying-extra-java-options)
    * [Requesting GPU Resources](#requesting-gpu-resources)
    * [Host Network](#host-network)    
    * [Mounting Secrets](#mounting-secrets)
    * [Mounting ConfigMaps](#mounting-configmaps)
        * [Mounting a ConfigMap storing Spark Configuration Files](#mounting-a-configmap-storing-spark-configuration-files)
        * [Mounting a ConfigMap storing Hadoop Configuration Files](#mounting-a-configmap-storing-hadoop-configuration-files)
    * [Mounting Volumes](#mounting-volumes)
    * [Using Secrets As Environment Variables](#using-secrets-as-environment-variables)
    * [Using Image Pull Secrets](#using-image-pull-secrets)
    * [Using Pod Affinity](#using-pod-affinity)
    * [Using Tolerations](#using-tolerations)
    * [Using Pod Security Context](#using-pod-security-context)
    * [Using Sidecar Containers](#using-sidecar-containers)
    * [Python Support](#python-support)
    * [Monitoring](#monitoring) 
* [Working with SparkApplications](#working-with-sparkapplications)
    * [Creating a New SparkApplication](#creating-a-new-sparkapplication)
    * [Deleting a SparkApplication](#deleting-a-sparkapplication)
    * [Updating a SparkApplication](#updating-a-sparkapplication)
    * [Checking a SparkApplication](#checking-a-sparkapplication)
    * [Configuring Automatic Application Restart](#configuring-automatic-application-restart)
    * [Configuring Automatic Application Re-submission on Submission Failures](#configuring-automatic-application-re-submission-on-submission-failures)
* [Running Spark Applications on a Schedule using a ScheduledSparkApplication](#running-spark-applications-on-a-schedule-using-a-scheduledsparkapplication)
* [Enabling Leader Election for High Availability](#enabling-leader-election-for-high-availability)
* [Customizing the Operator](#customizing-the-operator)

## Using a SparkApplication
The operator runs Spark applications specified in Kubernetes objects of the `SparkApplication` custom resource type. The most common way of using a `SparkApplication` is store the `SparkApplication` specification in a YAML file and use the `kubectl` command or alternatively the `sparkctl` command to work with the `SparkApplication`. The operator automatically submits the application as configured in a `SparkApplication` to run on the Kubernetes cluster and uses the `SparkApplication` to collect and surface the status of the driver and executors to the user.

## Writing a SparkApplication Spec

As with all other Kubernetes API objects, a `SparkApplication` needs the `apiVersion`, `kind`, and `metadata` fields. For general information about working with manifests, see [object management using kubectl](https://kubernetes.io/docs/concepts/overview/object-management-kubectl/overview/).

A `SparkApplication` also needs a [`.spec` section](https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status). This section contains fields for specifying various aspects of an application including its type (`Scala`, `Java`, `Python`, or `R`), deployment mode (`cluster` or `client`), main application resource URI (e.g., the URI of the application jar), main class, arguments, etc. Node selectors are also supported via the optional field `.spec.nodeSelector`.

It also has fields for specifying the unified container image (to use for both the driver and executors) and the image pull policy, namely, `.spec.image` and `.spec.imagePullPolicy` respectively. If a custom init-container (in both the driver and executor pods) image needs to be used, the optional field `.spec.initContainerImage` can be used to specify it. If set, `.spec.initContainerImage` overrides `.spec.image` for the init-container image. Otherwise, the image specified by `.spec.image` will be used for the init-container. It is invalid if both `.spec.image` and `.spec.initContainerImage` are not set.

Below is an example showing part of a `SparkApplication` specification:

```yaml
apiVersion: sparkoperator.k8s.io/v1beta1
kind: SparkApplication
metadata:
  name: spark-pi
  namespace: default
spec:
  type: Scala
  mode: cluster
  image: gcr.io/spark/spark:v2.4.0
  mainClass: org.apache.spark.examples.SparkPi
  mainApplicationFile: local:///opt/spark/examples/jars/spark-examples_2.11-2.4.0.jar
```

### Specifying Application Dependencies

Often Spark applications need additional files additionally to the main application resource to run. Such application dependencies can include for example jars and data files the application needs at runtime. When using the `spark-submit` script to submit a Spark application, such dependencies are specified using the `--jars` and `--files` options. To support specification of application dependenies, a `SparkApplication` uses an optional field `.spec.deps` that in turn supports specifying jars and files, respectively. More specifically, the optional fields `.spec.deps.jars` and`.spec.deps.files` correspond to the `--jars` and `--files` options of the `spark-submit` script, respectively.

Additionally, `.spec.deps` also has fields for specifying the locations in the driver and executor containers where jars and files should be downloaded to, namely, `.spec.deps.jarsDownloadDir` and `.spec.deps.filesDownloadDir`. The optional fields `.spec.deps.downloadTimeout` and `.spec.deps.maxSimultaneousDownloads` are used to control the timeout and maximum parallelism of downloading dependencies that are hosted remotely, e.g., on an HTTP server, or in external storage such as HDFS, Google Cloud Storage, or AWS S3.

The following is an example specification with both container-local (i.e., within the container) and remote dependencies:

```yaml
spec:
  deps:
    jars:
      - local:///opt/spark-jars/gcs-connector.jar
    files:
      - gs://spark-data/data-file-1.txt
      - gs://spark-data/data-file-2.txt
```

### Specifying Spark Configuration

There are two ways to add Spark configuration: setting individual Spark configuration properties using the optional field `.spec.sparkConf` or mounting a special Kubernetes ConfigMap storing Spark configuration files (e.g. `spark-defaults.conf`, `spark-env.sh`, `log4j.properties`) using the optional field `.spec.sparkConfigMap`. If `.spec.sparkConfigMap` is used, additionally to mounting the ConfigMap into the driver and executors, the operator additionally sets the environment variable `SPARK_CONF_DIR` to point to the mount path of the ConfigMap.

```yaml
spec:
  sparkConf:
    "spark.ui.port": "4045"
    "spark.eventLog.enabled": "true"
    "spark.eventLog.dir": "hdfs://hdfs-namenode-1:8020/spark/spark-events"
```

### Specifying Hadoop Configuration

There are two ways to add Hadoop configuration: setting individual Hadoop configuration properties using the optional field `.spec.hadoopConf` or mounting a special Kubernetes ConfigMap storing Hadoop configuration files (e.g.  `core-site.xml`) using the optional field `.spec.hadoopConfigMap`. The operator automatically adds the prefix `spark.hadoop.` to the names of individual Hadoop configuration properties in `.spec.hadoopConf`. If  `.spec.hadoopConfigMap` is used, additionally to mounting the ConfigMap into the driver and executors, the operator additionally sets the environment variable `HADOOP_CONF_DIR` to point to the mount path of the ConfigMap.

The following is an example showing the use of individual Hadoop configuration properties:

```yaml
spec:
  hadoopConf:
    "fs.gs.project.id": spark
    "fs.gs.system.bucket": spark
    "google.cloud.auth.service.account.enable": true
    "google.cloud.auth.service.account.json.keyfile": /mnt/secrets/key.json
```

### Writing Driver Specification

The `.spec` section of a `SparkApplication` has a `.spec.driver` field for configuring the driver. It allows users to set the memory and CPU resources to request for the driver pod, and the container image the driver should use. It also has fields for optionally specifying labels, annotations, and environment variables for the driver pod. By default, the driver pod name of an application is automatically generated by the Spark submission client. If instead you want to use a particular name for the driver pod, the optional field `.spec.driver.podName` can be used. The driver pod by default uses the `default` service account in the namespace it is running in to talk to the Kubernetes API server. The `default` service account, however, may or may not have sufficient permissions to create executor pods and the headless service used by the executors to connect to the driver. If it does not and a custom service account that has the right permissions should be used instead, the optional field `.spec.driver.serviceAccount` can be used to specify the name of the custom service account. When a custom container image is needed for the driver, the field `.spec.driver.image` can be used to specify it. This overrides the image specified in `.spec.image` if it is also set. It is invalid if both `.spec.image` and `.spec.driver.image` are not set.

For applications that need to mount Kubernetes [Secrets](https://kubernetes.io/docs/concepts/configuration/secret/) or [ConfigMaps](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/) into the driver pod, fields `.spec.driver.secrets` and `.spec.driver.configMaps` can be used. For more details, please refer to 
[Mounting Secrets](#mounting-secrets) and [Mounting ConfigMaps](#mounting-configmaps).

The following is an example driver specification:

```yaml
spec:
  driver:
    cores: 0.1
    coreLimit: 200m
    memory: 512m
    labels:
      version: 2.4.0
    serviceAccount: spark
```

### Writing Executor Specification

The `.spec` section of a `SparkApplication` has a `.spec.executor` field for configuring the executors. It allows users to set the memory and CPU resources to request for the executor pods, and the container image the executors should use. It also has fields for optionally specifying labels, annotations, and environment variables for the executor pods. By default, a single executor is requested for an application. If more than one executor are needed, the optional field `.spec.executor.instances` can be used to specify the number of executors to request. When a custom container image is needed for the executors, the field `.spec.executor.image` can be used to specify it. This overrides the image specified in `.spec.image` if it is also set. It is invalid if both `.spec.image` and `.spec.executor.image` are not set.

For applications that need to mount Kubernetes [Secrets](https://kubernetes.io/docs/concepts/configuration/secret/) or [ConfigMaps](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/) into the executor pods, fields `.spec.executor.secrets` and `.spec.executor.configMaps` can be used. For more details, please refer to 
[Mounting Secrets](#mounting-secrets) and [Mounting ConfigMaps](#mounting-configmaps).

An example executor specification is shown below:

```yaml
spec:
  executor:
    cores: 1
    instances: 1
    memory: 512m
    labels:
      version: 2.4.0
```

### Specifying Extra Java Options

A `SparkApplication` can specify extra Java options for the driver or executors, using the optional field `.spec.driver.javaOptions` for the driver and `.spec.executor.javaOptions` for executors. Below is an example:

```yaml
spec:
  executor:
    javaOptions: "-XX:+UnlockExperimentalVMOptions -XX:+UseCGroupMemoryLimitForHeap"
```

Values specified using those two fields get converted to Spark configuration properties `spark.driver.extraJavaOptions` and `spark.executor.extraJavaOptions`, respectively. **Prefer using the above two fields over configuration properties `spark.driver.extraJavaOptions` and `spark.executor.extraJavaOptions`** as the fields work well with other fields that might modify what gets set for `spark.driver.extraJavaOptions` or `spark.executor.extraJavaOptions`.

### Requesting GPU Resources

A `SparkApplication` can specify GPU resources for the driver or executor pod, using the optional field `.spec.driver.gpu` or `.spec.executor.gpu`. Below is an example:

```yaml
spec:
  driver:
    cores: 0.1
    coreLimit: "200m"
    memory: "512m"
    gpu:
      name: "amd.com/gpu"   # GPU resource name
      quantity: 1           # number of GPUs to request
    labels:
      version: 2.4.0
    serviceAccount: spark
  executor:
    cores: 1
    instances: 1
    memory: "512m"
    gpu:
      name: "nvidia.com/gpu"
      quantity: 1
```
Note that the mutating admission webhook is needed to use this feature. Please refer to the [Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.

### Host Network

A `SparkApplication` can specify `hostNetwork` for the driver or executor pod, using the optional field `.spec.driver.hostNetwork` or `.spec.executor.hostNetwork`. When `hostNetwork` is `true`, the operator sets pods' `spec.hostNetwork` to `true` and sets pods' `spec.dnsPolicy` to `ClusterFirstWithHostNet`. Below is an example:

```yaml
spec:
  driver:
    cores: 0.1
    coreLimit: "200m"
    memory: "512m"
    hostNetwork: true
    labels:
      version: 2.4.0
    serviceAccount: spark
  executor:
    cores: 1
    instances: 1
    memory: "512m"
```
Note that the mutating admission webhook is needed to use this feature. Please refer to the [Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.


### Mounting Secrets

As mentioned above, both the driver specification and executor specification have an optional field `secrets` for configuring the list of Kubernetes Secrets to be mounted into the driver and executors, respectively. The field is a map with the names of the Secrets as keys and values specifying the mount path and type of each Secret. For instance, the following example shows a driver specification with a Secret named `gcp-svc-account` of type `GCPServiceAccount` to be mounted to `/mnt/secrets` in the driver pod.

```yaml
spec:
  driver:
    secrets:
      - name: gcp-svc-account
        path: /mnt/secrets
        secretType: GCPServiceAccount
```

The type of a Secret as specified by the `secretType` field is a hint to the operator on what extra configuration it needs to take care of for the specific type of Secrets. For example, if a Secret is of type **`GCPServiceAccount`**, the operator additionally sets the environment variable **`GOOGLE_APPLICATION_CREDENTIALS`** to point to the JSON key file stored in the secret. Please refer to 
[Getting Started with Authentication](https://cloud.google.com/docs/authentication/getting-started) for more information on how to authenticate with GCP services using a service account JSON key file. Note that the operator assumes that the key of the service account JSON key file in the Secret data map is **`key.json`** so it is able to set the environment variable automatically. Similarly, if the type of a Secret is **`HadoopDelegationToken`**, the operator additionally sets the environment variable **`HADOOP_TOKEN_FILE_LOCATION`** to point to the file storing the Hadoop delegation token. In this case, the operator assumes that the key of the delegation token file in the Secret data map is **`hadoop.token`**.
The `secretType` field should have the value `Generic` if no extra configuration is required.

### Mounting ConfigMaps

Both the driver specification and executor specifications have an optional field for configuring
the list of Kubernetes ConfigMaps to be mounted into the driver and executors, respectively. The field is a map with keys being the names of the ConfigMaps and values specifying the mount path of each ConfigMap. For instance, the following example shows a driver specification with a ConfigMap named `configmap1` to be mounted to `/mnt/config-maps` in the driver pod.

```yaml
spec:
  driver:
    configMaps:
      - name: configmap1
        path: /mnt/config-maps
```

Note that the mutating admission webhook is needed to use this feature. Please refer to the [Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.

#### Mounting a ConfigMap storing Spark Configuration Files

A `SparkApplication` can specify a Kubernetes ConfigMap storing Spark configuration files such as `spark-env.sh` or `spark-defaults.conf` using the optional field `.spec.sparkConfigMap` whose value is the name of the ConfigMap. The ConfigMap is assumed to be in the same namespace as that of the `SparkApplication`. The operator mounts the ConfigMap onto path `/etc/spark/conf` in both the driver and executors. Additionally, it also sets the environment variable `SPARK_CONF_DIR` to point to `/etc/spark/conf` in the driver and executors.

Note that the mutating admission webhook is needed to use this feature. Please refer to the 
[Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.

#### Mounting a ConfigMap storing Hadoop Configuration Files

A `SparkApplication` can specify a Kubernetes ConfigMap storing Hadoop configuration files such as `core-site.xml` using the optional field `.spec.hadoopConfigMap` whose value is the name of the ConfigMap. The ConfigMap is assumed to be in the same namespace as that of the `SparkApplication`. The operator mounts the ConfigMap onto path  `/etc/hadoop/conf` in both the driver and executors. Additionally, it also sets the environment variable `HADOOP_CONF_DIR` to point to `/etc/hadoop/conf` in the driver and executors.

Note that the mutating admission webhook is needed to use this feature. Please refer to the [Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.

### Mounting Volumes

The operator also supports mounting user-specified Kubernetes volumes into the driver and executors. A 
`SparkApplication` has an optional field `.spec.volumes` for specifying the list of [volumes](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.9/#volume-v1-core) the driver and the executors need collectively. Then both the driver and executor specifications have an optional field `volumeMounts`  that specifies the [volume mounts](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.9/#volumemount-v1-core) for the volumes needed by the driver and executors, respectively. The following is an example showing a `SparkApplication` with both driver and executor volume mounts.

```yaml
spec:
  volumes:
    - name: spark-data
      persistentVolumeClaim:
        claimName: my-pvc
    - name: spark-work
      emptyDir: {}
  driver:
    volumeMounts:
      - name: spark-work
        mountPath: /mnt/spark/work
  executor:
    volumeMounts:
      - name: spark-data
        mountPath: /mnt/spark/data
      - name: spark-work
        mountPath: /mnt/spark/work
    
```

Note that the mutating admission webhook is needed to use this feature. Please refer to the [Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.

### Using Secrets As Environment Variables

**Note that this feature requires an image based on the latest Spark master branch.** 

A `SparkApplication` can use [secrets as environment variables](https://kubernetes.io/docs/concepts/configuration/secret/#using-secrets-as-environment-variables), through the optional field `.spec.driver.envSecretKeyRefs` for the driver pod and the optional field 
`.spec.executor.envSecretKeyRefs` for the executor pods. A `envSecretKeyRefs` is a map from environment variable names to pairs consisting of a secret name and a secret key. Below is an example:

```yaml
spec:
  driver:
    envSecretKeyRefs:
      SECRET_USERNAME:
        name: mysecret
        key: username
      SECRET_PASSWORD:
        name: mysecret
        key: password 
```

### Using Image Pull Secrets

**Note that this feature requires an image based on the latest Spark master branch.**

For images that need image-pull secrets to be pulled, a `SparkApplication` has an optional field `.spec.imagePullSecrets` for specifying a list of image-pull secrets. Below is an example:

```yaml
spec:
  imagePullSecrets:
    - secret1
    - secret2
```

### Using Pod Affinity

A `SparkApplication` can specify an `Affinity` for the driver or executor pod, using the optional field `.spec.driver.affinity` or `.spec.executor.affinity`. Below is an example:

```yaml
spec:
  driver:
    affinity:
      podAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          ...   
  executor:
    affinity:
      podAntiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          ... 
```

Note that the mutating admission webhook is needed to use this feature. Please refer to the [Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.

### Using Tolerations

A `SparkApplication` can specify an `Tolerations` for the driver or executor pod, using the optional field `.spec.driver.tolerations` or `.spec.executor.tolerations`. Below is an example:

```yaml
spec:
  driver:
    tolerations:
    - key: Key
      operator: Exists
      effect: NoSchedule
            
  executor:
    tolerations:
    - key: Key
      operator: Equal
      value: Value
      effect: NoSchedule    
```

Note that the mutating admission webhook is needed to use this feature. Please refer to the 
[Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.

### Using Pod Security Context

A `SparkApplication` can specify a `PodSecurityContext` for the driver or executor pod, using the optional field `.spec.driver.securityContext` or `.spec.executor.securityContext`. Below is an example:

```yaml
spec:
  driver:
    securityContext:
      runAsUser: 1000    
  executor:
    securityContext:
      runAsUser: 1000
```

Note that the mutating admission webhook is needed to use this feature. Please refer to the 
[Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.

### Using Sidecar Containers

A `SparkApplication` can specify one or more optional sidecar containers for the driver or executor pod, using the optional field `.spec.driver.sidecars` or `.spec.executor.containers`. The specification of each sidecar container follows the [Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.14/#container-v1-core) API definition. Below is an example:

```yaml
spec:
  driver:
    sidecars:
    - name: "sidecar1"
      image: "sidecar1:latest"
      ...  
  executor:
    sidecars:
    - name: "sidecar1"
      image: "sidecar1:latest"
      ...
```

### Using DNS Settings
A `SparkApplication` can define DNS settings for the driver and/or executor pod, by adding the standart [DNS](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#pod-s-dns-config) kubernetes settings. Fields to add such configuration are `.spec.driver.dnsConfig` and `.spec.executor.dnsConfig`. Example:

```yaml
spec:
  driver:
    dnsConfig:
      nameservers:
        - 1.2.3.4
      searches:
        - ns1.svc.cluster.local
        - my.dns.search.suffix
      options:
        - name: ndots
          value: "2"
        - name: edns0
```

Note that the mutating admission webhook is needed to use this feature. Please refer to the 
[Quick Start Guide](quick-start-guide.md) on how to enable the mutating admission webhook.

### Python Support

Python support can be enabled by setting `.spec.mainApplicationFile` with path to your python application. Optionaly, the `.spec.pythonVersion` field can be used to set the major Python version of the docker image used to run the driver and executor containers. Below is an example showing part of a `SparkApplication` specification:

```yaml
spec:
  type: Python
  pythonVersion: 2
  mainApplicationFile: local:///opt/spark/examples/src/main/python/pyfiles.py
```

Some PySpark applications need additional Python packages to run. Such dependencies are specified using the optional field `.spec.deps.pyFiles` , which translates to the `--py-files` option of the spark-submit command.

```yaml
spec:
  deps:
    pyFiles:
       - local:///opt/spark/examples/src/main/python/py_container_checks.py
       - gs://spark-data/python-dep.zip
```

In order to use the dependencies that are hosted remotely, the following PySpark code can be used in Spark 2.4.

```
python_dep_file_path = SparkFiles.get("python-dep.zip")
spark.sparkContext.addPyFile(dep_file_path)
```

Note that Python binding for PySpark is available in Apache Spark 2.4.

### Monitoring

The operator supports using the Spark metric system to expose metrics to a variety of sinks. Particularly, it is able to automatically configure the metric system to expose metrics to [Prometheus](https://prometheus.io/). Specifically, the field `.spec.monitoring` specifies how application monitoring is handled and particularly how metrics are to be reported. The metric system is configured through the configuration file `metrics.properties`, which gets its content from the field `.spec.monitoring.metricsProperties`. The content of [metrics.properties](../spark-docker/conf/metrics.properties) will be used by default if `.spec.monitoring.metricsProperties` is not specified. You can choose to enable or disable reporting driver and executor metrics using the fields `.spec.monitoring.exposeDriverMetrics` and `.spec.monitoring.exposeExecutorMetrics`, respectively. 

Further, the field `.spec.monitoring.prometheus` specifies how metrics are exposed to Prometheus using the [Prometheus JMX exporter](https://github.com/prometheus/jmx_exporter). When `.spec.monitoring.prometheus` is specified, the operator automatically configures the JMX exporter to run as a Java agent. The only required field of `.spec.monitoring.prometheus` is `jmxExporterJar`, which specified the path to the Prometheus JMX exporter Java agent jar in the container. If you use the image `gcr.io/spark-operator/spark:v2.4.0-gcs-prometheus`, the jar is located at `/prometheus/jmx_prometheus_javaagent-0.11.0.jar`. The field `.spec.monitoring.prometheus.port` specifies the port the JMX exporter Java agent binds to and defaults to `8090` if not specified. The field `.spec.monitoring.prometheus.configuration` specifies the content of the configuration to be used with the JMX exporter. The content of [prometheus.yaml](../spark-docker/conf/prometheus.yaml) will be used by default if `.spec.monitoring.prometheus.configuration` is not specified.    

Below is an example that shows how to configure the metric system to expose metrics to Prometheus using the Prometheus JMX exporter. Note that the JMX exporter Java agent jar is listed as a dependency and will be downloaded to where `.spec.dep.jarsDownloadDir` points to in Spark 2.3.x, which is `/var/spark-data/spark-jars` by default. Things are different in Spark 2.4 as dependencies will be downloaded to the local working directory instead in Spark 2.4. A complete example can be found in [examples/spark-pi-prometheus.yaml](../examples/spark-pi-prometheus.yaml).

```yaml
spec:
  dep:
    jars:
    - http://central.maven.org/maven2/io/prometheus/jmx/jmx_prometheus_javaagent/0.11.0/jmx_prometheus_javaagent-0.11.0.jar
  monitoring:
    exposeDriverMetrics: true
    prometheus:
      jmxExporterJar: "/var/spark-data/spark-jars/jmx_prometheus_javaagent-0.11.0.jar"    
```

The operator automatically adds the annotations such as `prometheus.io/scrape=true` on the driver and/or executor pods (depending on the values of  `.spec.monitoring.exposeDriverMetrics` and `.spec.monitoring.exposeExecutorMetrics`) so the metrics exposed on the pods can be scraped by the Prometheus server in the same cluster.

## Working with SparkApplications

### Creating a New SparkApplication

A `SparkApplication` can be created from a YAML file storing the `SparkApplication` specification using either the `kubectl apply -f <YAML file path>` command or the `sparkctl create <YAML file path>` command. Please refer to the `sparkctl` [README](../sparkctl/README.md#create) for usage of the `sparkctl create` command. Once a `SparkApplication` is successfully created, the operator will receive it and submits the application as configured in the specification to run on the Kubernetes cluster.

### Deleting a SparkApplication

A `SparkApplication` can be deleted using either the `kubectl delete <name>` command or the `sparkctl delete <name>` command. Please refer to the `sparkctl` [README](../sparkctl/README.md#delete) for usage of the `sparkctl delete` 
command. Deleting a `SparkApplication` deletes the Spark application associated with it. If the application is running when the deletion happens, the application is killed and all Kubernetes resources associated with the application are deleted or garbage collected. 

### Updating a SparkApplication

A `SparkApplication` can be updated using the `kubectl apply -f <updated YAML file>` command. When a `SparkApplication`  is successfully updated, the operator will receive both the updated and old `SparkApplication` objects. If the specification of the `SparkApplication` has changed, the operator submits the application to run, using the updated specification. If the application is currently running, the operator kills the running application before submitting a new run with the updated specification. There is planned work to enhance the way `SparkApplication` updates are handled. For example, if the change was to increase the number of executor instances, instead of killing the currently running application and starting a new run, it is a much better user experience to incrementally launch the additional executor pods.

### Checking a SparkApplication

A `SparkApplication` can be checked using the `kubectl describe sparkapplications <name>` command. The output of the command shows the specification and status of the `SparkApplication` as well as events associated with it. The events communicate the overall process and errors of the `SparkApplication`. 

### Configuring Automatic Application Restart and Failure Handling

The operator supports automatic application restart with a configurable `RestartPolicy` using the optional field
`.spec.restartPolicy`. The following is an example of a sample `RestartPolicy`:

 ```yaml
  restartPolicy:
     type: OnFailure
     onFailureRetries: 3
     onFailureRetryInterval: 10
     onSubmissionFailureRetries: 5
     onSubmissionFailureRetryInterval: 20
```
The valid types of restartPolicy include `Never`, `OnFailure`, and `Always`. Upon termination of an application,
the operator determines if the application is subject to restart based on its termination state and the
`RestartPolicy` in the specification. If the application is subject to restart, the operator restarts it by
submitting a new run of it. For `OnFailure`, the Operator further supports setting limits on number of retries
via the `onFailureRetries` and `onSubmissionFailureRetries` fields. Additionally, if the  submission retries has not been reached,
the operator retries submitting the application using a linear backoff with the interval specified by
`onFailureRetryInterval` and `onSubmissionFailureRetryInterval` which are required for both `OnFailure` and `Always` `RestartPolicy`.
The old resources like driver pod, ui service/ingress etc. are deleted if it still exists before submitting the new run, and a new  driver pod is created by the submission
client so effectively the driver gets restarted.

## Running Spark Applications on a Schedule using a ScheduledSparkApplication 

The operator supports running a Spark application on a standard [cron](https://en.wikipedia.org/wiki/Cron) schedule using objects of the `ScheduledSparkApplication` custom resource type. A `ScheduledSparkApplication` object specifies a cron schedule on which the application should run and a `SparkApplication` template from which a `SparkApplication` object for each run of the application is created. The following is an example `ScheduledSparkApplication`:

```yaml
apiVersion: "sparkoperator.k8s.io/v1beta1"
kind: ScheduledSparkApplication
metadata:
  name: spark-pi-scheduled
  namespace: default
spec:
  schedule: "@every 5m"
  concurrencyPolicy: Allow
  successfulRunHistoryLimit: 1
  failedRunHistoryLimit: 3
  template:
    type: Scala
    mode: cluster
    image: gcr.io/spark/spark:v2.4.0
    mainClass: org.apache.spark.examples.SparkPi
    mainApplicationFile: local:///opt/spark/examples/jars/spark-examples_2.11-2.3.0.jar
    driver:
      cores: 0.5
      memory: 512m
    executor:
      cores: 1
      instances: 1
      memory: 512m
    restartPolicy:
      type: Never
```

The concurrency of runs of an application is controlled by `.spec.concurrencyPolicy`, whose valid values are `Allow`, `Forbid`, and `Replace`, with `Allow` being the default. The meanings of each value is described below:
* `Allow`: more than one run of an application are allowed if for example the next run of the application is due even though the previous run has not completed yet.
* `Forbid`: no more than one run of an application is allowed. The next run of the application can only start if the previous run has completed.
* `Replace`: no more than one run of an application is allowed. When the next run of the application is due, the previous run is killed and the next run starts as a replacement.

A scheduled `ScheduledSparkApplication` can be temporarily suspended (no future scheduled runs of the application will be triggered) by setting `.spec.suspend` to `true`. The schedule can be resumed by removing `.spec.suspend` or setting it to `false`. A `ScheduledSparkApplication` can have names of `SparkApplication` objects for the past runs of the application tracked in the `Status` section as discussed below. The numbers of past successful runs and past failed runs to keep track of are controlled by field `.spec.successfulRunHistoryLimit` and field `.spec.failedRunHistoryLimit`, respectively. The example above allows 1 past successful run and 3 past failed runs to be tracked.

The `Status` section of a `ScheduledSparkApplication` object shows the time of the last run and the proposed time of the next run of the application, through `.status.lastRun` and `.status.nextRun`, respectively. The names of the `SparkApplication` object for the most recent run (which may  or may not be running) of the application are stored in `.status.lastRunName`. The names of `SparkApplication` objects of the past successful runs of the application are stored in `.status.pastSuccessfulRunNames`. Similarly, the names of `SparkApplication` objects of the past failed runs of the application are stored in `.status.pastFailedRunNames`.

Note that certain restart policies (specified in `.spec.template.restartPolicy`) may not work well with the specified schedule and concurrency policy of a `ScheduledSparkApplication`. For example, a restart policy of `Always` should never be used with a `ScheduledSparkApplication`. In most cases, a restart policy of `OnFailure` may not be a good choice as the next run usually picks up where the previous run left anyway. For these reasons, it's often the right choice to use a restart policy of `Never` as the example above shows. 

## Enabling Leader Election for High Availability

The operator supports a high-availability (HA) mode, in which there can be more than one replicas of the operator, with only one of the replicas (the leader replica) actively operating. If the leader replica fails, the leader election process is engaged again to determine a new leader from the replicas available. The HA mode can be enabled through an optional leader election process. Leader election is disabled by default but can be enabled via a command-line flag. The following table summarizes the command-line flags relevant to leader election:

| Flag | Default Value | Description |
| ------------- | ------------- | ------------- |
| `leader-election` | `false` | Whether to enable leader election (or the HA mode) or not. |
| `leader-election-lock-namespace` | `spark-operator` | Kubernetes namespace of the lock resource used for leader election. |
| `leader-election-lock-name` | `spark-operator-lock` | Name of the ock resource used for leader election. |
| `leader-election-lease-duration` | 15 seconds | Leader election lease duration. |
| `leader-election-renew-deadline` | 14 seconds | Leader election renew deadline. |
| `leader-election-retry-period` | 4 seconds | Leader election retry period. |

## Customizing the Operator

To customize the operator, you can follow the steps below:

1. Compile Spark distribution with Kubernetes support as per [Spark documentation](https://spark.apache.org/docs/latest/building-spark.html#building-with-kubernetes-support).
2. Create docker images to be used for Spark with [docker-image tool](https://spark.apache.org/docs/latest/running-on-kubernetes.html#docker-images).
3. Create a new operator image based on the above image. You need to modify the `FROM` tag in the [Dockerfile](https://github.com/GoogleCloudPlatform/spark-on-k8s-operator/blob/master/Dockerfile) with your Spark image.
4. Build and push your operator image built above. 
5. Deploy the new image by modifying the [/manifest/spark-operator.yaml](https://github.com/GoogleCloudPlatform/spark-on-k8s-operator/blob/master/manifest/spark-operator.yaml) file and specfiying your operator image.

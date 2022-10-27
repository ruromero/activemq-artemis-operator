#!/bin/bash

# service accounts, role/rolebinding, operator resources
installDir="$1/install"
mkdir -p "${installDir}"

file=()
resource_kind=""
resource_name=""

# write a single yaml.
# it takes one parameter:
# 1 array that contains total lines of the yaml
function writeFile() {
  array_name=$1[@]
  lines=("${!array_name}")

  case $resource_kind in
    CustomResourceDefinition)
      if [[ ${resource_name} =~ (activemqartemises) ]]; then
        createFile "$installDir/010_crd_artemis.yaml"
      elif [[ ${resource_name} =~ (activemqartemissecurities) ]]; then
        createFile "$installDir/020_crd_artemis_security.yaml"
      elif [[ ${resource_name} =~ (activemqartemisaddresses) ]]; then
        createFile "$installDir/030_crd_artemis_address.yaml"
      elif [[ ${resource_name} =~ (activemqartemisscaledowns) ]]; then
        createFile "$installDir/040_crd_artemis_scaledown.yaml"
      else
        createFile "$installDir/${resource_name}.yaml"
      fi
      ;;

    Deployment)
      createFile "$installDir/110_operator.yaml"
      ;;

    Role)
      if [[ ${resource_name} =~ (operator) ]]; then
        createFile "$installDir/060_namespace_role.yaml"
        createFile "$installDir/060_cluster_role.yaml"
        sed -i 's/kind: Role/kind: ClusterRole/' \
          "$installDir/060_cluster_role.yaml"
      elif [[ ${resource_name} =~ (leader-election) ]]; then
        createFile "$installDir/080_election_role.yaml"
      else
        createFile "$installDir/${resource_name}.yaml"
      fi
      ;;

    RoleBinding)
      if [[ ${resource_name} =~ (operator) ]]; then
        createFile "$installDir/070_namespace_role_binding.yaml"
        createFile "$installDir/070_cluster_role_binding.yaml"
        sed -i -e 's/kind: Role/kind: ClusterRole/' \
          -e 's/kind: RoleBinding/kind: ClusterRoleBinding/' \
          "$installDir/070_cluster_role_binding.yaml"
        echo '  namespace: activemq-artemis-operator' >> \
          "$installDir/070_cluster_role_binding.yaml"
      elif [[ ${resource_name} =~ (leader-election) ]]; then
        createFile "$installDir/090_election_role_binding.yaml"
      else
        createFile "$installDir/${resource_name}.yaml"
      fi
      ;;

    ServiceAccount)
      createFile "$installDir/050_service_account.yaml"
      ;;

    ConfigMap)
      createFile "$installDir/100_operator_config.yaml"
      ;;

    Namespace)
      echo "Skipping ${resource_kind}:${resource_name}"
      ;;
    
    *)
      createFile "$installDir/${resource_kind}_${resource_name}.yaml"
      ;;
    esac
}

function createFile() {
  echo "Writing $1"
  rm -rf $1
  for value in "${file[@]}"; do
    printf "%s\n" "${value}" >> $1
  done
}

function beginFile() {
  if [[ ${#file[@]} > 0 ]]
  then
    # it gathered a single file now
    writeFile file
  fi
  file=()
}

function appendFile() {
  file+=("$1")
}

IFS=''
while read -r line
do
  if [[ $line =~ ^---$ ]]
  then
    beginFile
    resource_kind=""
    resource_name=""
    meta_start="false"
  else
    to_append="true"

    if [[ $resource_kind == "" && $line =~ ^(kind: ) ]]
    then
      resource_kind=$(echo $line | cut -b 7-)
    fi

    if [[ $line =~ ^(metadata:) ]]
    then
      meta_start="true"
    elif [[ ! $line =~ ^("  ") ]]; then
      meta_start="false"
    elif [[ ${meta_start} == "true" ]]; then
      if [[ $line =~ ^(  name: ) ]]; then
        resource_name=$(echo $line | cut -b 9-)
      elif [[ $line =~ ^(  namespace: ) ]]; then
        to_append="false"
      fi
    fi

    if [[ $resource_kind == "RoleBinding" ]]; then
      if [[ $line =~ ^(subjects:) ]]; then
        rb_subjects="true"
      elif [[ ${rb_subjects} == "true" ]]; then
        if [[ $line =~ ^(  namespace: ) ]]; then
          to_append="false"
        fi
      fi
    fi

    if [[ $to_append == "true" ]]; then
      appendFile "$line"
    fi
  fi
done
# check the last one
beginFile

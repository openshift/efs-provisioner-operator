FROM alpine:3.6

RUN adduser -D efs-provisioner-operator
USER efs-provisioner-operator

ADD tmp/_output/bin/efs-provisioner-operator /usr/local/bin/efs-provisioner-operator

# Assay 运行镜像：静态单二进制（内嵌前端）+ alpine 提供 CA 证书与时区数据。
# 构建上下文只含编译好的 assay 二进制（release workflow 中的 stage/ 目录）。
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 assay
COPY assay /usr/local/bin/assay
USER assay
EXPOSE 8321
ENTRYPOINT ["assay"]
CMD ["serve"]

CREATE TABLE IF NOT EXISTS `image_cache` (
    `id` PRIMARE KEY AUTO_INCREMENT COMMENT '主键id',
    `name` VARCHAR(128) NOT NULL COMMENT '缓存名',
    `ref` VARCHAR(64) NOT NULL COMMENT '镜像',
    `auth` VARCHAR(64) NOT NULL COMMENT '仓库访问凭证',
    `creator` VARCHAR(64) NOT NULL COMMENT '创建者',
    `region` VARCHAR(16) NOT NULL COMMENT '地域',
    `capacity` INT NOT NULL COMMENT '容量',
    `disk_usage` INT NOT NULL COMMENT '使用量',
    `state` TINYINT NOT NULL COMMENT '状态 0可用 1删除',
    `create_time` TIMESTAMP NOT NULL DEFAULT CURRENT_TIME COMMENT '创建时间',
    `update_time` TIMESTAMP NOT NULL DEFAULT CURRENT_TIME COMMENT '更新时间',
    INDEX `ix_c_r_r` (`creator`, `region`, `ref`)
) ENGINE=InnoDB;
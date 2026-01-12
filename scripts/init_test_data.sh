#!/bin/bash
# 测试数据初始化脚本

# 1. 在博客数据库中插入测试文章
echo "=== 向博客数据库插入测试文章 ==="
mysql -h 127.0.0.1 -P 13316 -u root -proot webook_article << 'EOF'
-- 插入测试文章（如果不存在）
INSERT INTO published_articles (id, title, content, author_id, status, ctime, utime)
VALUES 
(1001, 'Go语言并发编程', 'Go语言的并发模型基于CSP（Communicating Sequential Processes）。goroutine是Go语言中的轻量级线程，channel用于goroutine之间的通信。使用go关键字可以启动一个新的goroutine。', 1, 2, UNIX_TIMESTAMP(), UNIX_TIMESTAMP()),
(1002, 'Redis向量数据库实战', 'Redis Stack提供了向量搜索功能，可以用于构建RAG（检索增强生成）系统。通过HNSW算法实现高效的近似最近邻搜索。', 1, 2, UNIX_TIMESTAMP(), UNIX_TIMESTAMP()),
(1003, 'Kubernetes入门指南', 'Kubernetes是一个开源的容器编排平台，用于自动化部署、扩展和管理容器化应用。Pod是K8s中最小的调度单位。', 1, 2, UNIX_TIMESTAMP(), UNIX_TIMESTAMP())
ON DUPLICATE KEY UPDATE utime = UNIX_TIMESTAMP();
EOF

echo "=== 博客数据库测试文章插入完成 ==="

# 2. 在GopherAI数据库中更新用户的blog_author_id
echo "=== 更新GopherAI用户的blog_author_id ==="
mysql -h 127.0.0.1 -P 3306 -u root -p123456 GopherAI << 'EOF'
-- 查看现有用户
SELECT id, username, blog_author_id FROM users LIMIT 5;

-- 为第一个用户关联博客 author_id = 1
UPDATE users SET blog_author_id = 1 WHERE id = 1;

-- 确认更新
SELECT id, username, blog_author_id FROM users WHERE blog_author_id IS NOT NULL;
EOF

echo "=== 用户关联完成 ==="
echo ""
echo "测试数据创建成功！请启动 GopherAI 服务来触发文章同步。"

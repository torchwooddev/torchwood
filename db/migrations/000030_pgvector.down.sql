-- 对称回滚：仅按本库 DROP 扩展。已建 vector 列的表会因类型依赖而失败，
-- 需先 DROP 依赖列/表（POC 无存量数据，直接重建不双读）。
DROP EXTENSION IF EXISTS vector;

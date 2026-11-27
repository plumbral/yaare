package files

import "context"

func MergeBilateralText(ctx context.Context, left, right FS, leftPath, rightPath, leftVersion, rightVersion, content, nonce string) error {
	l, e := ReadText(ctx, left, leftPath)
	if e != nil {
		return e
	}
	r, e := ReadText(ctx, right, rightPath)
	if e != nil {
		return e
	}
	if l.Version != leftVersion || r.Version != rightVersion {
		return problem("文件在合并期间变化，未保存，请重新读取。")
	}
	bom := false
	result, e := saveTextConverted(ctx, left, leftPath, l.Version, content, nonce+"-local", "auto", "UTF-8", &bom, "LF")
	if e != nil {
		return e
	}
	if result.Status != "saved" {
		return problem("本地内容已变化，未保存，请重新读取。")
	}
	result, e = saveTextConverted(ctx, right, rightPath, r.Version, content, nonce+"-remote", "auto", "UTF-8", &bom, "LF")
	if e != nil || result.Status != "saved" {
		return problem("本地已保存，远程未确认保存；请重新读取两端并处理。")
	}
	return nil
}

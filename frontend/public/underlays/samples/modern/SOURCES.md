# 现代风格户型图（本地验证用）

这些图是从国内房产 / 装修网站上的公开页面抓的，用来手动验证「底图导入 + 两点定标」在真实
素材上的表现：彩色填充、家具图例、尺寸链、水印、非白底，都是自产合成图覆盖不到的情况。

**权利状态未知**：没有找到可核实的开放许可。它们**只用于本地演示与手工验证**，不随课程
交付物分发，项目也不对这些图片主张任何权利。要公开分发请先替换成 `../synthetic/` 里自产的
图，或 `..` 下标注 Public Domain 的档案图纸。

| 文件 | 内容 | 尺寸 | 来源 |
| --- | --- | --- | --- |
| `zh-cad-duplex-1f.jpg` | 跃层一层黑白 CAD 平面，带尺寸链与「1楼3.0米」标注 | 1024×1448 | <https://img.zx123.cn/Resources/zx123cn/uploadfile/2015/0415/f8828407da6db317d2d0cef536f511d2.jpg> |
| `zh-color-big-flat.jpg` | 彩色精装大平层，含家具与各房间面积 | 1800×1800 | <https://jr-prod-cms-assets.oss-cn-beijing.aliyuncs.com/Asset/836d55d1-2fc9-42c5-98e0-16e60d2b1af5.jpg> |
| `zh-color-2bed.jpg` | 白底彩色两居（客厅 / 卧室 / 厨卫） | 800×800 | <http://qhyxpic.oss.kujiale.com/fpimgnew/2016/03/29/Vvp_kAozGsowTxE_800x800.jpg> |
| `zh-color-2bed-dims.jpg` | 彩色两居 + 四周完整尺寸链 | 920×950 | <https://img.zx123.cn/Resources/zx123cn/uploadfile/2015/0612/b04a6af8f0f37e40acf55572de0818e6.jpg> |
| `zh-cad-btype-dims.jpg` | 「B 户型装修参考图」黑白 CAD，带尺寸 | 1000×999 | <https://img.zx123.cn/Resources/zx123cn/uploadfile/2015/0612/9fdee195d69336063bbe40c43d9aa6b6.jpg> |
| `zh-color-narrow.jpg` | 窄长户型彩色平面（走廊式布局） | 1800×1800 | <https://jr-prod-cms-assets.oss-cn-beijing.aliyuncs.com/Asset/5ccb5bb4-312c-438b-acf7-7acd0f2dfb9a.jpg> |
| `zh-structural-floor.jpg` | 结构 / 建筑平面，无家具、细线密集 | 1273×1800 | <https://image2.znzmo.com/1691078415919_3715.png> |
| `zh-floor-plate.jpg` | 整层楼平面（绿底，多户拼在一起） | 1280×887 | <http://i5.szhomeimg.com/o/2023/04/04/04041021432951916.PNG> |

## 重新抓取

链家 / 贝壳的户型图页需要登录（`huxingtu` 会 302 到 `clogin.ke.com`），所以走的是图片搜索：

```bash
curl -s "https://cn.bing.com/images/search?q=%E4%B8%89%E5%AE%A4%E4%B8%A4%E5%8E%85%E6%88%B7%E5%9E%8B%E5%9B%BE" \
  -A "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36" \
  | grep -oE 'murl&quot;:&quot;[^&]+' | sed 's/murl&quot;:&quot;//'
```

直连 CDN 要带 `-A` 与 `-e https://cn.bing.com/`，否则部分站点返回 403。落盘前统一转 JPEG
（quality 86）、长边压到 1800px，单张 70–210KB。

# 本轮来源与许可

- 新增业务代码、UI、SQL、测试和脚本均在本轮独立实现；未复制其他开源项目源码、截图、字体或教材。
- go.mod/go.sum/package-lock.json未新增依赖。沿用 go-fsrs/v4 v4.0.0，Go模块下载元数据指向原仓库 https://github.com/open-spaced-repetition/go-fsrs ，完整提交 `61159b9b3891b1279b63206afef4086d7ef92992`（refs/tags/v4.0.0）。没有复制其源码到本仓库，调度只调用原依赖。
- 审计来源：用户提供 learning_v3_1_S0_review_docs.zip，四份文档原样置于docs/v3；v3.1边界参考PR #13，未自动合并。v2已在最新main，因此不重建Gin/队列/FSRS/题目版本/词库。
- 项目根目录仍没有LICENSE；未替权利人宣称全仓MIT。第三方许可治理需按来源台账单独处理。
- 合成题、音频魔数、mock供应商和示例来源仅为测试夹具，不冒充真实教材、官方高考试卷、官方语音评分或实付费用证据。

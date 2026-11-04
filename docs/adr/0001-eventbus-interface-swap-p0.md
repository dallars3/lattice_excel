# ADR 0001：EventBus 接口可替换，P0 不实现 Kafka 适配器

日期：2026-11-05
状态：已接受

## 背景

spec.md:81 与 ticket 02 原表述"Kafka 可切换"在 P0 评审与实现之间存在歧义：P0 约束为单机自托管、零外部依赖试运行，Kafka 适配器不可能在 P0 被部署或验收；但接口抽象已按可切换设计（libs/eventbus.EventBus，Redis PubSub/Streams 与内存实现共用）。

## 决策

1. "可切换"的 P0 交付口径 = **EventBus 接口抽象 + 内存/Redis 两实现**；切换提供方不改业务代码，这一性质由接口签名保证。
2. Kafka 适配器推迟到多机部署阶段按同一接口补齐；在此之前 spec/ticket 措辞统一为"总线接口可替换"。
3. libs/eventbus 中不得出现 Kafka 专属 API 泄漏到业务代码。

## 影响

- ticket 02 验收第 4 条按上述口径解释为已满足（接口抽象 + 两实现）。
- 未来补 Kafka 适配器时仅新增 libs/eventbus 实现，不触碰业务模块。

# Skills（Agent Skills）

Coren 支持标准 **Agent Skills**：一个目录 + 一份 `SKILL.md`，模型按需加载。
这既保持上下文精简（**渐进披露**），又让能力包可以像普通文件一样版本化、分享。

## 结构

```
skills/
└── git-commit/
    ├── SKILL.md          # 必需：frontmatter + 指令正文
    └── reference.txt     # 可选：SKILL.md 里引用的资源
```

`SKILL.md`：

```markdown
---
name: git-commit
description: Write a clear, conventional git commit message for staged changes.
---

# Writing a commit message

1. Run `git diff --cached --stat` ...
```

- `name`：可选。缺省用目录名。
- `description`：**必需**。这是模型做发现时唯一看到的内容。
- `disable-model-invocation: true`：可选。从模型发现中隐藏，仅供显式调用。

## 加载路径

默认扫描工作目录下的 `.coren/skills`。可通过配置覆盖：

- 配置项 `skills_dir`
- 环境变量 `COREN_SKILLS_DIR`

嵌套目录也会扫描；**同名时浅层的优先**（项目内的覆盖 vendor 的）。

## 模型如何使用

启动时框架注册两个内置工具：

| 工具 | 作用 |
|---|---|
| `list_skills` | 列出可用 skill 的名称与描述（隐藏 `disable-model-invocation` 的） |
| `use_skill` | 加载某个 skill 的完整指令；可带 `resource` 读取其目录内的资源文件 |

典型流程：模型先 `list_skills`，判断相关后 `use_skill` 拉取正文，再据此行事。
全文只在被使用时进入上下文。

## 插件也能新增 skill

Skills 既是文件、也是注册项。任何插件都可以向 `skills` 服务注册：

```go
func (MyPlugin) Inject() []string { return []string{skills.Key} }

func (MyPlugin) Apply(ctx coren.Context) error {
    registry, _ := coren.UnwrapKey[skills.Service](ctx, skills.Key)
    registry.Register(skills.Skill{
        Name:        "my-skill",
        Description: "Contributed by a plugin.",
        Content:     "Do the thing.",
    })
    return nil
}
```

文件来源与代码来源共用同一个注册表，模型看到的是合并后的结果。

## 安全

`use_skill` 的资源读取限制在 skill 目录内，拒绝 `..` 逃逸与绝对路径。

## 示例

- `examples/skills/git-commit/`：一个可直接使用的 skill。
  复制到 `.coren/skills/` 即可被加载。

## 配置摘要

| 位置 | 说明 |
|---|---|
| `.coren/skills/` | 默认目录（工作目录下） |
| `COREN_SKILLS_DIR` | 覆盖目录 |
| `skills_dir`（coren.json） | 覆盖目录 |
| `skills` 服务键 | 插件直接注册 skill 的入口 |

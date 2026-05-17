import AppKit
import Carbon.HIToolbox
import Foundation

let defaultKeyCode: UInt32 = 37
let defaultMods: UInt32 = UInt32(cmdKey)
let udKeyCode = "tachyonHotkeyKeyCode"
let udMods = "tachyonHotkeyModifiers"

let bg = NSColor(srgbRed: 0.06, green: 0.065, blue: 0.075, alpha: 1)
let panelColor = NSColor(srgbRed: 0.095, green: 0.105, blue: 0.12, alpha: 1)
let ink = NSColor(srgbRed: 0.88, green: 0.93, blue: 0.95, alpha: 1)
let dim = NSColor(srgbRed: 0.52, green: 0.57, blue: 0.62, alpha: 1)
let accent = NSColor(srgbRed: 0.27, green: 0.78, blue: 0.72, alpha: 1)

var hotkeyRef: EventHotKeyRef?
var appDelegateRef: AppDelegate?

let hotkeyHandler: EventHandlerUPP = { _, _, _ -> OSStatus in
    DispatchQueue.main.async { appDelegateRef?.toggleWindow() }
    return noErr
}

struct InputOption: Codable {
    let value: String
    let label: String
}

struct InputField: Codable {
    let name: String
    let type: String
    let required: Bool
    let description: String?
    let `default`: JSONValue?
    let options: [InputOption]?
}

struct SpecSummary: Codable {
    let id: String
    let display_name: String
    let project: String?
    let role: String?
    let launch: String?
    let provider: String?
    let source_kind: String
    let description: String?
}

struct ListResponse: Codable {
    let specs: [SpecSummary]
    let mode: String
    let root: String
    let warnings: [String]?
}

struct DescribeResponse: Codable {
    let spec: SpecSummary
    let inputs: [InputField]
    let warnings: [String]?
}

enum JSONValue: Codable {
    case string(String)
    case bool(Bool)
    case number(Double)
    case int(Int)
    case null

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Int.self) {
            self = .int(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else {
            self = .string(try container.decode(String.self))
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .string(let value): try container.encode(value)
        case .bool(let value): try container.encode(value)
        case .number(let value): try container.encode(value)
        case .int(let value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }

    var stringValue: String {
        switch self {
        case .string(let value): return value
        case .bool(let value): return value ? "true" : "false"
        case .number(let value): return String(value)
        case .int(let value): return String(value)
        case .null: return ""
        }
    }

    var boolValue: Bool {
        switch self {
        case .bool(let value): return value
        case .string(let value): return ["1", "true", "yes", "on"].contains(value.lowercased())
        case .int(let value): return value != 0
        case .number(let value): return value != 0
        case .null: return false
        }
    }
}

enum EngineError: Error {
    case missingEngine
    case invalidOutput(String)
}

final class EngineClient {
    func listSpecs() throws -> ListResponse {
        try runJSON(["list"], type: ListResponse.self)
    }

    func describe(specID: String) throws -> DescribeResponse {
        try runJSON(["describe", specID], type: DescribeResponse.self)
    }

    func launchCommand(specID: String, inputs: [String: String]) throws -> String {
        var parts = [shellQuote(try enginePath()), "launch", shellQuote(specID)]
        for key in inputs.keys.sorted() {
            guard let value = inputs[key], !value.isEmpty else { continue }
            parts.append("--input")
            parts.append(shellQuote("\(key)=\(value)"))
        }
        return parts.joined(separator: " ")
    }

    private func runJSON<T: Decodable>(_ args: [String], type: T.Type) throws -> T {
        let process = Process()
        let out = Pipe()
        let err = Pipe()
        process.executableURL = URL(fileURLWithPath: try enginePath())
        process.arguments = args
        process.standardOutput = out
        process.standardError = err
        try process.run()
        process.waitUntilExit()

        let stdout = String(data: out.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""
        let stderr = String(data: err.fileHandleForReading.readDataToEndOfFile(), encoding: .utf8) ?? ""

        guard process.terminationStatus == 0 else {
            throw NSError(domain: "TachyonEngine", code: Int(process.terminationStatus), userInfo: [
                NSLocalizedDescriptionKey: stderr.isEmpty ? "tachyon-engine failed" : stderr.trimmingCharacters(in: .whitespacesAndNewlines)
            ])
        }

        guard let data = stdout.data(using: .utf8) else {
            throw EngineError.invalidOutput("tachyon-engine produced non-UTF8 output")
        }
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw EngineError.invalidOutput(stdout)
        }
    }

    private func enginePath() throws -> String {
        if let bundled = Bundle.main.resourceURL?.appendingPathComponent("tachyon-engine").path,
           FileManager.default.isExecutableFile(atPath: bundled) {
            return bundled
        }
        let fallback = NSString(string: "~/dev/hollis-labs/apps/tachyon/dist/Tachyon.app/Contents/Resources/tachyon-engine").expandingTildeInPath
        if FileManager.default.isExecutableFile(atPath: fallback) {
            return fallback
        }
        throw EngineError.missingEngine
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    var statusItem: NSStatusItem!
    var windowController: LauncherWindowController?
    var eventHandlerRef: EventHandlerRef?

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        setupStatusItem()
        registerHotkey()
    }

    func applicationWillTerminate(_ notification: Notification) {
        unregisterHotkey()
    }

    func setupStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        if let button = statusItem.button {
            button.image = NSImage(systemSymbolName: "bolt.horizontal.circle", accessibilityDescription: "Tachyon")
            button.image?.isTemplate = true
            button.target = self
            button.action = #selector(statusClicked)
        }
    }

    @objc func statusClicked() {
        toggleWindow()
    }

    @objc func toggleWindow() {
        if let wc = windowController, wc.window?.isVisible == true {
            wc.hide()
        } else {
            showWindow()
        }
    }

    @objc func showWindow() {
        if windowController == nil { windowController = LauncherWindowController() }
        windowController?.show()
    }

    func registerHotkey() {
        unregisterHotkey()
        var eventType = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: OSType(kEventHotKeyPressed))
        InstallEventHandler(GetApplicationEventTarget(), hotkeyHandler, 1, &eventType, nil, &eventHandlerRef)
        let id = EventHotKeyID(signature: OSType(0x54414348), id: 1)
        RegisterEventHotKey(savedKeyCode(), savedModifiers(), id, GetApplicationEventTarget(), 0, &hotkeyRef)
    }

    func unregisterHotkey() {
        if let ref = hotkeyRef { UnregisterEventHotKey(ref); hotkeyRef = nil }
        if let ref = eventHandlerRef { RemoveEventHandler(ref); eventHandlerRef = nil }
    }
}

final class LauncherWindowController: NSObject, NSWindowDelegate, NSTableViewDataSource, NSTableViewDelegate {
    let engine = EngineClient()
    var allSpecs: [SpecSummary] = []
    var filteredSpecs: [SpecSummary] = []
    var selectedSpecID: String?
    var currentDescribe: DescribeResponse?

    var window: LauncherWindow!
    var tableView: NSTableView!
    var formStack: NSStackView!
    var statusLabel: NSTextField!
    var projectFilter: NSPopUpButton!
    var roleFilter: NSPopUpButton!
    var launchButton: NSButton!
    var controls: [String: NSControl] = [:]

    override init() {
        super.init()
        buildWindow()
        reloadSpecs()
    }

    func buildWindow() {
        window = LauncherWindow(contentRect: NSRect(x: 0, y: 0, width: 980, height: 620), styleMask: [.borderless], backing: .buffered, defer: false)
        window.delegate = self
        window.backgroundColor = .clear
        window.isOpaque = false
        window.hasShadow = true
        window.level = .floating
        window.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        window.center()

        let root = RoundedRoot(frame: window.contentView?.bounds ?? .zero)
        root.autoresizingMask = [.width, .height]
        window.contentView = root

        let title = label("Tachyon", 19, accent, .semibold)
        title.frame = NSRect(x: 24, y: 578, width: 220, height: 24)
        root.addSubview(title)

        let subtitle = label("Project and role filters on the left. Enter launches the selected spec.", 12, dim, .regular)
        subtitle.frame = NSRect(x: 150, y: 580, width: 760, height: 20)
        root.addSubview(subtitle)

        projectFilter = NSPopUpButton(frame: NSRect(x: 24, y: 536, width: 220, height: 28), pullsDown: false)
        roleFilter = NSPopUpButton(frame: NSRect(x: 256, y: 536, width: 200, height: 28), pullsDown: false)
        configurePopup(projectFilter)
        configurePopup(roleFilter)
        projectFilter.target = self
        roleFilter.target = self
        projectFilter.action = #selector(filterChanged)
        roleFilter.action = #selector(filterChanged)
        root.addSubview(projectFilter)
        root.addSubview(roleFilter)

        let refreshButton = NSButton(title: "Refresh", target: self, action: #selector(refreshPressed))
        refreshButton.frame = NSRect(x: 470, y: 536, width: 90, height: 28)
        refreshButton.isBordered = false
        refreshButton.contentTintColor = accent
        root.addSubview(refreshButton)

        tableView = makeTable()
        let left = makeScroll(tableView)
        left.frame = NSRect(x: 24, y: 72, width: 360, height: 448)
        root.addSubview(left)

        let rightPanel = NSView(frame: NSRect(x: 404, y: 72, width: 552, height: 448))
        rightPanel.wantsLayer = true
        rightPanel.layer?.backgroundColor = panelColor.cgColor
        rightPanel.layer?.cornerRadius = 10
        root.addSubview(rightPanel)

        let formScroll = NSScrollView(frame: NSRect(x: 0, y: 0, width: 552, height: 448))
        formScroll.drawsBackground = false
        formScroll.hasVerticalScroller = true
        formScroll.borderType = .noBorder

        formStack = NSStackView()
        formStack.orientation = .vertical
        formStack.alignment = .leading
        formStack.spacing = 12
        formStack.edgeInsets = NSEdgeInsets(top: 18, left: 18, bottom: 18, right: 18)

        let document = NSView(frame: NSRect(x: 0, y: 0, width: 552, height: 448))
        document.addSubview(formStack)
        formStack.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            formStack.leadingAnchor.constraint(equalTo: document.leadingAnchor),
            formStack.topAnchor.constraint(equalTo: document.topAnchor),
            formStack.widthAnchor.constraint(equalToConstant: 516),
            formStack.bottomAnchor.constraint(equalTo: document.bottomAnchor)
        ])
        formScroll.documentView = document
        rightPanel.addSubview(formScroll)

        launchButton = NSButton(title: "Launch in iTerm2", target: self, action: #selector(launchPressed))
        launchButton.frame = NSRect(x: 816, y: 28, width: 140, height: 30)
        launchButton.bezelStyle = .rounded
        root.addSubview(launchButton)

        statusLabel = label("", 12, dim, .regular)
        statusLabel.frame = NSRect(x: 24, y: 28, width: 770, height: 24)
        root.addSubview(statusLabel)
    }

    func show() {
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)
        window.orderFrontRegardless()
        window.makeFirstResponder(tableView)
    }

    func hide() {
        window.orderOut(nil)
    }

    @objc func refreshPressed() {
        reloadSpecs()
    }

    @objc func filterChanged() {
        applyFilters()
    }

    func reloadSpecs() {
        do {
            let response = try engine.listSpecs()
            allSpecs = response.specs
            populateFilters()
            applyFilters()
            if let warning = response.warnings?.first {
                updateStatus(warning)
            } else {
                updateStatus("Loaded \(allSpecs.count) spec(s) from \(response.root)")
            }
        } catch {
            allSpecs = []
            filteredSpecs = []
            tableView.reloadData()
            rebuildForm(nil)
            updateStatus(error.localizedDescription)
        }
    }

    func populateFilters() {
        let projects = Array(Set(allSpecs.compactMap { $0.project?.isEmpty == false ? $0.project : nil })).sorted()
        let roles = Array(Set(allSpecs.compactMap { $0.role?.isEmpty == false ? $0.role : nil })).sorted()

        projectFilter.removeAllItems()
        roleFilter.removeAllItems()
        projectFilter.addItems(withTitles: ["All Projects"] + projects)
        roleFilter.addItems(withTitles: ["All Roles"] + roles)
    }

    func applyFilters() {
        let project = projectFilter.titleOfSelectedItem == "All Projects" ? nil : projectFilter.titleOfSelectedItem
        let role = roleFilter.titleOfSelectedItem == "All Roles" ? nil : roleFilter.titleOfSelectedItem
        filteredSpecs = allSpecs.filter { spec in
            let matchesProject = project == nil || spec.project == project
            let matchesRole = role == nil || spec.role == role
            return matchesProject && matchesRole
        }
        tableView.reloadData()

        if filteredSpecs.isEmpty {
            selectedSpecID = nil
            rebuildForm(nil)
            updateStatus("No specs match the current filters")
            return
        }

        let selectedRow = filteredSpecs.firstIndex { $0.id == selectedSpecID } ?? 0
        tableView.selectRowIndexes(IndexSet(integer: selectedRow), byExtendingSelection: false)
        loadDescribe(for: filteredSpecs[selectedRow])
    }

    func loadDescribe(for spec: SpecSummary) {
        selectedSpecID = spec.id
        do {
            let describe = try engine.describe(specID: spec.id)
            currentDescribe = describe
            rebuildForm(describe)
            updateStatus("\(describe.spec.display_name) · \(describe.spec.project ?? "no project") · \(describe.spec.role ?? "no role")")
        } catch {
            currentDescribe = nil
            rebuildForm(nil)
            updateStatus(error.localizedDescription)
        }
    }

    func numberOfRows(in tableView: NSTableView) -> Int {
        filteredSpecs.count
    }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let field = NSTextField(labelWithString: "")
        field.font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
        field.textColor = ink
        field.backgroundColor = .clear
        field.lineBreakMode = .byTruncatingTail
        let spec = filteredSpecs[row]
        let project = spec.project ?? "no-project"
        let role = spec.role ?? "no-role"
        field.stringValue = "\(spec.display_name)  [\(project) / \(role)]"
        return field
    }

    func tableView(_ tableView: NSTableView, rowViewForRow row: Int) -> NSTableRowView? {
        LauncherRowView()
    }

    func tableViewSelectionDidChange(_ notification: Notification) {
        let row = tableView.selectedRow
        guard row >= 0, row < filteredSpecs.count else { return }
        loadDescribe(for: filteredSpecs[row])
    }

    @objc func launchPressed() {
        guard let describe = currentDescribe else {
            updateStatus("No spec selected")
            return
        }

        var inputs: [String: String] = [:]
        for field in describe.inputs {
            if let button = controls[field.name] as? NSButton {
                inputs[field.name] = button.state == .on ? "true" : "false"
            } else if let popup = controls[field.name] as? NSPopUpButton {
                if let selected = popup.selectedItem?.representedObject as? String {
                    inputs[field.name] = selected
                } else {
                    inputs[field.name] = popup.titleOfSelectedItem ?? ""
                }
            } else if let textField = controls[field.name] as? NSTextField {
                inputs[field.name] = textField.stringValue
            }
        }

        do {
            let command = try engine.launchCommand(specID: describe.spec.id, inputs: inputs)
            openITerm(command)
            hide()
        } catch {
            updateStatus(error.localizedDescription)
        }
    }

    func rebuildForm(_ describe: DescribeResponse?) {
        controls = [:]
        formStack.arrangedSubviews.forEach {
            formStack.removeArrangedSubview($0)
            $0.removeFromSuperview()
        }

        guard let describe else {
            formStack.addArrangedSubview(formTitle("No Spec Selected"))
            formStack.addArrangedSubview(formBody("Pick a spec from the left pane."))
            return
        }

        formStack.addArrangedSubview(formTitle(describe.spec.display_name))
        let meta = "\(describe.spec.project ?? "no project") · \(describe.spec.role ?? "no role") · \(describe.spec.source_kind)"
        formStack.addArrangedSubview(formBody(meta))
        if let description = describe.spec.description, !description.isEmpty {
            formStack.addArrangedSubview(formBody(description))
        }
        if let warnings = describe.warnings, !warnings.isEmpty {
            formStack.addArrangedSubview(formBody(warnings.joined(separator: "\n")))
        }

        if describe.inputs.isEmpty {
            formStack.addArrangedSubview(formBody("This spec does not expose typed inputs."))
            return
        }

        for field in describe.inputs {
            let row = NSStackView()
            row.orientation = .vertical
            row.alignment = .leading
            row.spacing = 6

            row.addArrangedSubview(formLabel("\(field.name)\(field.required ? " *" : "")"))
            if let description = field.description, !description.isEmpty {
                row.addArrangedSubview(formHint(description))
            }

            switch field.type {
            case "bool":
                let button = NSButton(checkboxWithTitle: "Enabled", target: nil, action: nil)
                button.state = field.default?.boolValue == true ? .on : .off
                button.contentTintColor = ink
                controls[field.name] = button
                row.addArrangedSubview(button)
            case "enum":
                let popup = NSPopUpButton(frame: NSRect(x: 0, y: 0, width: 320, height: 28), pullsDown: false)
                configurePopup(popup)
                let options = field.options ?? []
                popup.addItems(withTitles: options.map(\.label))
                for (index, option) in options.enumerated() {
                    popup.item(at: index)?.representedObject = option.value
                    if option.value == field.default?.stringValue {
                        popup.selectItem(at: index)
                    }
                }
                controls[field.name] = popup
                row.addArrangedSubview(popup)
            default:
                let input = NSTextField(frame: NSRect(x: 0, y: 0, width: 420, height: 28))
                input.stringValue = field.default?.stringValue ?? ""
                input.isBordered = true
                input.isBezeled = true
                controls[field.name] = input
                row.addArrangedSubview(input)
            }

            formStack.addArrangedSubview(row)
        }
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        hide()
        return false
    }

    func updateStatus(_ text: String) {
        statusLabel.stringValue = text
    }

    private func makeTable() -> NSTableView {
        let table = LauncherTable()
        table.launcher = self
        table.headerView = nil
        table.backgroundColor = panelColor
        table.selectionHighlightStyle = .regular
        table.rowHeight = 28
        table.dataSource = self
        table.delegate = self
        let col = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("spec-col"))
        col.width = 344
        table.addTableColumn(col)
        return table
    }

    private func makeScroll(_ table: NSTableView) -> NSScrollView {
        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = true
        scroll.backgroundColor = panelColor
        scroll.borderType = .noBorder
        scroll.wantsLayer = true
        scroll.layer?.cornerRadius = 8
        return scroll
    }
}

final class LauncherTable: NSTableView {
    weak var launcher: LauncherWindowController?

    override func keyDown(with event: NSEvent) {
        switch event.keyCode {
        case 36:
            launcher?.launchPressed()
        case 53:
            launcher?.hide()
        default:
            super.keyDown(with: event)
        }
    }
}

final class LauncherRowView: NSTableRowView {
    override var isEmphasized: Bool {
        get { true }
        set {}
    }

    override func drawSelection(in dirtyRect: NSRect) {
        let rect = NSRect(x: bounds.minX + 4, y: bounds.midY - 10, width: bounds.width - 8, height: 20)
        accent.withAlphaComponent(0.82).setFill()
        NSBezierPath(roundedRect: rect, xRadius: 5, yRadius: 5).fill()
    }
}

final class LauncherWindow: NSWindow {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { true }

    override func keyDown(with event: NSEvent) {
        guard let controller = delegate as? LauncherWindowController else {
            super.keyDown(with: event)
            return
        }
        switch event.keyCode {
        case 53:
            controller.hide()
        case 36:
            controller.launchPressed()
        default:
            super.keyDown(with: event)
        }
    }
}

final class RoundedRoot: NSView {
    override var mouseDownCanMoveWindow: Bool { true }
    override func draw(_ dirtyRect: NSRect) {
        bg.setFill()
        NSBezierPath(roundedRect: bounds, xRadius: 12, yRadius: 12).fill()
    }
}

func savedKeyCode() -> UInt32 {
    let value = UserDefaults.standard.integer(forKey: udKeyCode)
    return value == 0 ? defaultKeyCode : UInt32(value)
}

func savedModifiers() -> UInt32 {
    let value = UserDefaults.standard.integer(forKey: udMods)
    return value == 0 ? defaultMods : UInt32(value)
}

func label(_ text: String, _ size: CGFloat, _ color: NSColor, _ weight: NSFont.Weight) -> NSTextField {
    let field = NSTextField(labelWithString: text)
    field.font = NSFont.monospacedSystemFont(ofSize: size, weight: weight)
    field.textColor = color
    field.backgroundColor = .clear
    field.isBezeled = false
    field.isEditable = false
    return field
}

func formTitle(_ text: String) -> NSView {
    label(text, 18, ink, .semibold)
}

func formBody(_ text: String) -> NSView {
    let field = NSTextField(wrappingLabelWithString: text)
    field.font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
    field.textColor = dim
    field.maximumNumberOfLines = 0
    return field
}

func formLabel(_ text: String) -> NSView {
    label(text, 12, ink, .medium)
}

func formHint(_ text: String) -> NSView {
    let field = NSTextField(wrappingLabelWithString: text)
    field.font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    field.textColor = dim
    field.maximumNumberOfLines = 0
    return field
}

func configurePopup(_ popup: NSPopUpButton) {
    popup.font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
    popup.contentTintColor = ink
}

func shellQuote(_ value: String) -> String {
    "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
}

func openITerm(_ command: String) {
    let script = """
    tell application id "com.googlecode.iterm2"
      activate
      if (count of windows) = 0 then
        create window with default profile
      else
        tell current window
          create tab with default profile
        end tell
      end if
      tell current session of current window
        write text \(appleScriptString(command))
      end tell
    end tell
    """
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
    process.arguments = ["-e", script]
    try? process.run()
}

func appleScriptString(_ value: String) -> String {
    "\"" + value.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"") + "\""
}

let app = NSApplication.shared
let delegate = AppDelegate()
appDelegateRef = delegate
app.delegate = delegate
app.run()

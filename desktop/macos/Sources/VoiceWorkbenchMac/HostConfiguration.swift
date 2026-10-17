import Foundation

struct HostConfiguration {
    static let speakerEnvironmentKeys = [
        "YOVOICE_SPEAKER_PYTHON": "PYTHON",
        "YOVOICE_SPEAKER_MODEL": "MODEL",
        "YOVOICE_SPEAKER_RUNNER": "RUNNER",
        "YOVOICE_SPEAKER_THRESHOLD": "THRESHOLD",
        "YOVOICE_SPEAKER_MARGIN": "MARGIN",
        "YOVOICE_SPEAKER_TIMEOUT": "TIMEOUT",
    ]

    let root: URL
    let shouldPrepareData: Bool
    let speakerEnvironment: [String: String]

    init(environment: [String: String], bundleInfo: [String: Any], homeDirectory: URL) {
        let defaultRoot = homeDirectory.appendingPathComponent(".yovoice", isDirectory: true)
        let validation = bundleInfo["YovoiceSpeakerValidation"] as? Bool == true
        let configuredRoot = environment["WORKBENCH_DATA"].flatMap { value in
            value.isEmpty ? nil : URL(fileURLWithPath: value, isDirectory: true)
        }
        let validationRoot = (bundleInfo["YovoiceValidationDataDirectory"] as? String).flatMap { value in
            value.isEmpty ? nil : URL(fileURLWithPath: value, isDirectory: true)
        }

        if let configuredRoot {
            root = configuredRoot
            shouldPrepareData = false
        } else if validation, let validationRoot {
            root = validationRoot
            shouldPrepareData = false
        } else {
            root = defaultRoot
            shouldPrepareData = true
        }

        let runtime = bundleInfo["YovoiceSpeakerRuntime"] as? [String: String] ?? [:]
        var values: [String: String] = [:]
        for (environmentName, plistName) in Self.speakerEnvironmentKeys {
            if let value = environment[environmentName] {
                values[environmentName] = value
            } else if let value = runtime[plistName] {
                values[environmentName] = value
            }
        }
        speakerEnvironment = values
    }

    static func requestTimeout(for method: String) -> TimeInterval {
        switch method {
        case "speaker.enroll", "speaker.identify", "speaker.generate":
            return 660
        default:
            return 120
        }
    }
}

/**
 * Minimal protobuf decoder for Meshtastic ChannelSet URLs
 * Decodes the URL fragment into human-readable channel configuration
 */

/**
 * Decode a Meshtastic channel URL
 * @param {string} url - Full Meshtastic URL (e.g., https://meshtastic.org/e/#...)
 * @returns {Object|null} Decoded channel set or null if invalid
 */
export function decodeMeshtasticURL(url) {
    try {
        // Extract the fragment (everything after #)
        const hashIndex = url.indexOf('#');
        if (hashIndex === -1) return null;

        const fragment = url.substring(hashIndex + 1);
        if (!fragment) return null;

        // Base64url decode
        const bytes = base64urlDecode(fragment);
        if (!bytes || bytes.length === 0) return null;

        return decodeChannelSet(bytes);
    } catch (e) {
        console.error('Failed to decode Meshtastic URL:', e);
        return null;
    }
}

/**
 * Base64url decode (RFC 4648 section 5)
 */
function base64urlDecode(str) {
    // Replace URL-safe chars with standard base64
    let base64 = str.replace(/-/g, '+').replace(/_/g, '/');
    // Add padding
    while (base64.length % 4) {
        base64 += '=';
    }
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
}

/**
 * Decode ChannelSet protobuf message
 */
function decodeChannelSet(bytes) {
    const reader = new ProtobufReader(bytes);
    const channels = [];
    let lora = null;

    while (reader.hasMore()) {
        const { fieldNumber, wireType } = reader.readTag();

        if (fieldNumber === 1 && wireType === 2) {
            // ChannelSettings (length-delimited)
            const channelBytes = reader.readBytes();
            channels.push(decodeChannelSettings(channelBytes));
        } else if (fieldNumber === 2 && wireType === 2) {
            // LoRaConfig (length-delimited)
            const loraBytes = reader.readBytes();
            lora = decodeLoRaConfig(loraBytes);
        } else {
            reader.skipField(wireType);
        }
    }

    return { channels, lora };
}

/**
 * Decode ChannelSettings protobuf message
 */
function decodeChannelSettings(bytes) {
    const reader = new ProtobufReader(bytes);
    const channel = {
        channelIndex: 0,
        psk: null,
        pskBytes: 0,
        name: '',
        uplinkEnabled: false,
        downlinkEnabled: false,
    };

    while (reader.hasMore()) {
        const { fieldNumber, wireType } = reader.readTag();

        switch (fieldNumber) {
            case 1: channel.channelIndex = reader.readVarint(); break;
            case 2:
                channel.psk = reader.readBytes();
                channel.pskBytes = channel.psk.length;
                break;
            case 3: channel.name = reader.readString(); break;
            case 5: channel.uplinkEnabled = reader.readVarint() !== 0; break;
            case 6: channel.downlinkEnabled = reader.readVarint() !== 0; break;
            default: reader.skipField(wireType);
        }
    }

    return channel;
}

/**
 * Decode LoRaConfig protobuf message
 */
function decodeLoRaConfig(bytes) {
    const reader = new ProtobufReader(bytes);
    const lora = {
        modemPreset: 0,
        bandwidth: 0,
        spreadFactor: 0,
        codingRate: 0,
        region: 0,
        hopLimit: 3,
        txEnabled: true,
        txPower: 0,
        channelNum: 0,
    };

    while (reader.hasMore()) {
        const { fieldNumber, wireType } = reader.readTag();

        switch (fieldNumber) {
            case 1: lora.modemPreset = reader.readVarint(); break;
            case 3: lora.bandwidth = reader.readVarint(); break;
            case 4: lora.spreadFactor = reader.readVarint(); break;
            case 5: lora.codingRate = reader.readVarint(); break;
            case 7: lora.region = reader.readVarint(); break;
            case 8: lora.hopLimit = reader.readVarint(); break;
            case 9: lora.txEnabled = reader.readVarint() !== 0; break;
            case 10: lora.txPower = reader.readVarint(); break;
            case 11: lora.channelNum = reader.readVarint(); break;
            default: reader.skipField(wireType);
        }
    }

    return lora;
}

/**
 * Minimal protobuf wire format reader
 */
class ProtobufReader {
    constructor(bytes) {
        this.bytes = bytes;
        this.pos = 0;
    }

    hasMore() {
        return this.pos < this.bytes.length;
    }

    readTag() {
        const value = this.readVarint();
        return {
            fieldNumber: value >>> 3,
            wireType: value & 0x7,
        };
    }

    readVarint() {
        let result = 0;
        let shift = 0;
        while (this.pos < this.bytes.length) {
            const byte = this.bytes[this.pos++];
            result |= (byte & 0x7f) << shift;
            if ((byte & 0x80) === 0) break;
            shift += 7;
        }
        return result >>> 0; // Unsigned
    }

    readBytes() {
        const length = this.readVarint();
        const bytes = this.bytes.slice(this.pos, this.pos + length);
        this.pos += length;
        return bytes;
    }

    readString() {
        const bytes = this.readBytes();
        return new TextDecoder().decode(bytes);
    }

    skipField(wireType) {
        switch (wireType) {
            case 0: this.readVarint(); break;        // Varint
            case 1: this.pos += 8; break;            // 64-bit
            case 2: {                                  // Length-delimited
                const len = this.readVarint();
                this.pos += len;
                break;
            }
            case 5: this.pos += 4; break;            // 32-bit
            default: break;
        }
    }
}

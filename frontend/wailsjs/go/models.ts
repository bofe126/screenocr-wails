export namespace main {
	
	export class Config {
	    trigger_delay_ms: number;
	    hotkey: string;
	    auto_copy: boolean;
	    show_debug: boolean;
	    image_preprocess: boolean;
	    ocr_engine: string;
	    enable_translation: boolean;
	    translation_source: string;
	    translation_target: string;
	    tencent_secret_id: string;
	    tencent_secret_key: string;
	    first_run: boolean;
	    show_welcome: boolean;
	    show_startup_notification: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trigger_delay_ms = source["trigger_delay_ms"];
	        this.hotkey = source["hotkey"];
	        this.auto_copy = source["auto_copy"];
	        this.show_debug = source["show_debug"];
	        this.image_preprocess = source["image_preprocess"];
	        this.ocr_engine = source["ocr_engine"];
	        this.enable_translation = source["enable_translation"];
	        this.translation_source = source["translation_source"];
	        this.translation_target = source["translation_target"];
	        this.tencent_secret_id = source["tencent_secret_id"];
	        this.tencent_secret_key = source["tencent_secret_key"];
	        this.first_run = source["first_run"];
	        this.show_welcome = source["show_welcome"];
	        this.show_startup_notification = source["show_startup_notification"];
	    }
	}

}

